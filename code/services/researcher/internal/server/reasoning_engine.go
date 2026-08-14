package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type reasoningEngineRequest struct {
	Input      map[string]any `json:"input"`
	Parameters map[string]any `json:"parameters"`
}

type reasoningEngineResponse struct {
	Output map[string]any `json:"output"`
}

func lookupString(fallback string, maps []map[string]any, keys ...string) string {
	for _, m := range maps {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
	}
	return fallback
}

func parseReasoningEngineRequest(reReq reasoningEngineRequest) (message, appName, userID, sessionID string) {
	maps := []map[string]any{reReq.Input, reReq.Parameters}

	message = lookupString("", []map[string]any{reReq.Input}, "message", "input")
	appName = lookupString("harbourmaster", maps, "appName", "app_name")
	userID = lookupString("default_user", maps, "userID", "user_id")
	sessionID = lookupString("default_session", maps, "sessionID", "session_id")
	return
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
		AppName:        appName,
		Agent:          a,
		SessionService: h.session,
	})
}

func (h *reasoningEngineHandlers) handle(w http.ResponseWriter, r *http.Request) {
	var reReq reasoningEngineRequest
	if err := json.NewDecoder(r.Body).Decode(&reReq); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	message, appName, userID, sessionID := parseReasoningEngineRequest(reReq)

	runr, err := h.newRunner(appName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := runr.Run(r.Context(), userID, sessionID, genai.NewContentFromText(message, "user"), agent.RunConfig{})

	var finalContent string
	for event, err := range resp {
		if err != nil {
			slog.Error("run error", "error", err)
			continue
		}
		if event.Content != nil && event.Content.Role == "model" {
			for _, part := range event.Content.Parts {
				if part.Text != "" && !part.Thought {
					finalContent += part.Text
				}
			}
		}
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

	f, _ := w.(http.Flusher)

	for event, err := range resp {
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %v\n\n", err)
			f.Flush()
			continue
		}

		var content string
		if event.Content != nil && event.Content.Role == "model" {
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
