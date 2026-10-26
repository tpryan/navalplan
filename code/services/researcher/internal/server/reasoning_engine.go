package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type reasoningEngineRequest struct {
	Input      any            `json:"input"`
	Parameters map[string]any `json:"parameters"`
}

type reasoningEngineResponse struct {
	Output map[string]any `json:"output"`
}

func extractText(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]any:
		for _, k := range []string{"prompt", "message", "input", "query", "text", "content"} {
			if sub, ok := v[k]; ok {
				if txt := extractText(sub); txt != "" {
					return txt
				}
			}
		}
		if parts, ok := v["parts"].([]any); ok {
			var sb strings.Builder
			for _, p := range parts {
				if text := extractText(p); text != "" {
					sb.WriteString(text)
				}
			}
			return sb.String()
		}
	case []any:
		var sb strings.Builder
		for _, item := range v {
			if text := extractText(item); text != "" {
				sb.WriteString(text)
			}
		}
		return sb.String()
	}
	return ""
}

func lookupString(fallback string, maps []map[string]any, keys ...string) string {
	for _, m := range maps {
		if m == nil {
			continue
		}
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
	}
	return fallback
}

func parseReasoningEngineRequest(reReq reasoningEngineRequest) (message, appName, userID, sessionID string) {
	var inputMap map[string]any
	if m, ok := reReq.Input.(map[string]any); ok {
		inputMap = m
	}
	maps := []map[string]any{inputMap, reReq.Parameters}

	message = extractText(reReq.Input)
	appName = lookupString("harbourmaster", maps, "appName", "app_name", "agent", "agent_name")
	userID = lookupString("default_user", maps, "userID", "user_id")
	sessionID = lookupString("default_session", maps, "sessionID", "session_id")
	return
}

func extractState(reReq reasoningEngineRequest) map[string]any {
	var inputMap map[string]any
	if m, ok := reReq.Input.(map[string]any); ok {
		inputMap = m
	}
	for _, m := range []map[string]any{inputMap, reReq.Parameters} {
		if m == nil {
			continue
		}
		if st, ok := m["state"].(map[string]any); ok && len(st) > 0 {
			return st
		}
	}
	return nil
}

func eventHasFunctionCall(event *session.Event) bool {
	if event == nil || event.Content == nil {
		return false
	}
	for _, part := range event.Content.Parts {
		if part != nil && part.FunctionCall != nil {
			return true
		}
	}
	return false
}

type reasoningEngineHandlers struct {
	loader  agent.Loader
	session session.Service
}

func (h *reasoningEngineHandlers) newRunner(appName string) (*runner.Runner, error) {
	a, err := h.loader.LoadAgent(appName)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return runner.New(runner.Config{
		AppName:           appName,
		Agent:             a,
		SessionService:    h.session,
		AutoCreateSession: true,
	})
}

func (h *reasoningEngineHandlers) ensureSessionWithState(r *http.Request, appName, userID, sessionID string, state map[string]any) {
	if h.session == nil || len(state) == 0 {
		return
	}
	_, _ = h.session.Create(r.Context(), &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
		State:     state,
	})
}

func (h *reasoningEngineHandlers) handle(w http.ResponseWriter, r *http.Request) {
	var reReq reasoningEngineRequest
	if err := json.NewDecoder(r.Body).Decode(&reReq); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	message, appName, userID, sessionID := parseReasoningEngineRequest(reReq)
	if sessionID == "" || sessionID == "default_session" {
		sessionID = fmt.Sprintf("re_%d", time.Now().UnixNano())
	}
	h.ensureSessionWithState(r, appName, userID, sessionID, extractState(reReq))

	runr, err := h.newRunner(appName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := runr.Run(r.Context(), userID, sessionID, genai.NewContentFromText(message, "user"), agent.RunConfig{})

	var finalContent string
	var lastErr error
	for event, err := range resp {
		if err != nil {
			slog.Error("reasoning engine run error", "error", err, "appName", appName, "sessionID", sessionID)
			lastErr = err
			continue
		}
		if eventHasFunctionCall(event) {
			continue
		}
		if event.Content != nil && (event.Content.Role == "" || event.Content.Role == "model") {
			for _, part := range event.Content.Parts {
				if part.Text != "" && !part.Thought {
					finalContent += part.Text
				}
			}
		}
	}

	if lastErr != nil && finalContent == "" {
		code := http.StatusInternalServerError
		if strings.Contains(lastErr.Error(), "429") || strings.Contains(lastErr.Error(), "RESOURCE_EXHAUSTED") || strings.Contains(lastErr.Error(), "Resource exhausted") {
			code = http.StatusTooManyRequests
		}
		http.Error(w, lastErr.Error(), code)
		return
	}

	res := reasoningEngineResponse{
		Output: map[string]any{
			"content":    finalContent,
			"session_id": sessionID,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (h *reasoningEngineHandlers) handleStream(w http.ResponseWriter, r *http.Request) {
	var reReq reasoningEngineRequest
	if err := json.NewDecoder(r.Body).Decode(&reReq); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	message, appName, userID, sessionID := parseReasoningEngineRequest(reReq)
	if sessionID == "" || sessionID == "default_session" {
		sessionID = fmt.Sprintf("re_%d", time.Now().UnixNano())
	}
	h.ensureSessionWithState(r, appName, userID, sessionID, extractState(reReq))

	runr, err := h.newRunner(appName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := runr.Run(r.Context(), userID, sessionID, genai.NewContentFromText(message, "user"), agent.RunConfig{
		StreamingMode: agent.StreamingModeSSE,
	})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	seenPartialInTurn := false
	for event, err := range resp {
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %v\n\n", err)
			f.Flush()
			continue
		}
		if eventHasFunctionCall(event) {
			seenPartialInTurn = false
			continue
		}
		if event.Partial {
			seenPartialInTurn = true
		} else if seenPartialInTurn {
			// Skip the aggregated full-turn model event emitted at the end of
			// an SSE-streamed turn to avoid duplicating the entire JSON payload.
			seenPartialInTurn = false
			continue
		}

		var content string
		if event.Content != nil && (event.Content.Role == "" || event.Content.Role == "model") {
			for _, part := range event.Content.Parts {
				if part.Text != "" && !part.Thought {
					content += part.Text
				}
			}
		}

		if content != "" {
			res := reasoningEngineResponse{
				Output: map[string]any{
					"content":    content,
					"session_id": sessionID,
				},
			}
			jsonData, _ := json.Marshal(res)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			f.Flush()
		}
	}
}
