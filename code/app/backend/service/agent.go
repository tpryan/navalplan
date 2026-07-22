package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const defaultAgentBaseURL = "http://127.0.0.1:8081"

// Resolver defines how to find the URL for a specific agent application.
type Resolver interface {
	Resolve(ctx context.Context, appName string) (string, error)
}

// StaticResolver returns a fixed base URL and appends the ADK API paths.
type StaticResolver struct {
	BaseURL string
}

func (s *StaticResolver) Resolve(ctx context.Context, appName string) (string, error) {
	url := s.BaseURL
	if url == "" {
		url = defaultAgentBaseURL
	}
	return url, nil
}

// AgentRunner handles communication with the AI agent service.
type AgentRunner struct {
	Client          *http.Client
	Resolver        Resolver
	ReasoningEngine *ReasoningEngineRunner
}

func (r *AgentRunner) getTarget(ctx context.Context, appName string) (string, error) {
	if r.Resolver == nil {
		return defaultAgentBaseURL, nil
	}
	return r.Resolver.Resolve(ctx, appName)
}

func (r *AgentRunner) isReasoningEngine(target string) bool {
	return strings.HasPrefix(target, "projects/") && strings.Contains(target, "/reasoningEngines/")
}

// agentRunRequest is the payload sent to /api/run.
type agentRunRequest struct {
	AppName    string `json:"appName"`
	UserID     string `json:"userId"`
	SessionID  string `json:"sessionId"`
	Stream     bool   `json:"stream"`
	NewMessage struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"newMessage"`
}

// AgentEvent is a single event in the agent's response stream.
type AgentEvent struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
		Role string `json:"role"`
	} `json:"content"`
}

// CreateSession creates an agent session. state is optional; pass nil for no
// session state.
func (r *AgentRunner) CreateSession(ctx context.Context, appName, userID, sessionID string, state map[string]any) error {
	target, err := r.getTarget(ctx, appName)
	if err != nil {
		return err
	}

	if r.isReasoningEngine(target) && r.ReasoningEngine != nil {
		return r.ReasoningEngine.CreateSession(ctx, target, appName, userID, sessionID, state)
	}

	url := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", target, appName, userID, sessionID)

	var body io.Reader
	if state != nil {
		b, _ := json.Marshal(map[string]any{"state": state})
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("build session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.Client.Do(req)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("agent session returned %d", resp.StatusCode)
	}
	return nil
}

// RunSync calls the agent without streaming and returns concatenated text from
// all model-role events. Use this for research/guide/discovery flows.
func (r *AgentRunner) RunSync(ctx context.Context, appName, userID, sessionID, prompt string) (string, error) {
	target, err := r.getTarget(ctx, appName)
	if err != nil {
		return "", err
	}

	if r.isReasoningEngine(target) && r.ReasoningEngine != nil {
		return r.ReasoningEngine.RunSync(ctx, target, appName, userID, sessionID, prompt)
	}

	body, err := r.buildRunBody(appName, userID, sessionID, prompt, false)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target+"/api/run", body)
	if err != nil {
		return "", fmt.Errorf("build run request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("run agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(b))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read agent response: %w", err)
	}

	var events []AgentEvent
	if err := json.Unmarshal(raw, &events); err != nil {
		slog.WarnContext(ctx, "Failed to decode agent response as JSON array", "error", err, "raw", string(raw))
		return "", fmt.Errorf("decode agent events: %w", err)
	}

	var sb strings.Builder
	for _, e := range events {
		if e.Content.Role == "model" && len(e.Content.Parts) > 0 {
			sb.WriteString(e.Content.Parts[0].Text)
		}
	}
	return sb.String(), nil
}

// RunStreaming calls the agent with streaming enabled and returns concatenated
// text from all event parts. It handles both NDJSON and JSON-array response
// formats. Use this for recommendation flows.
func (r *AgentRunner) RunStreaming(ctx context.Context, appName, userID, sessionID, prompt string) (string, error) {
	target, err := r.getTarget(ctx, appName)
	if err != nil {
		return "", err
	}

	if r.isReasoningEngine(target) && r.ReasoningEngine != nil {
		return r.ReasoningEngine.RunStreaming(ctx, target, appName, userID, sessionID, prompt)
	}

	body, err := r.buildRunBody(appName, userID, sessionID, prompt, true)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target+"/api/run", body)
	if err != nil {
		return "", fmt.Errorf("build run request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("run agent (streaming): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(b))
	}

	// Peek at the first byte to detect format (JSON array vs NDJSON).
	firstByte := make([]byte, 1)
	n, _ := resp.Body.Read(firstByte)

	var sb strings.Builder

	if n > 0 && firstByte[0] == '[' {
		rest, _ := io.ReadAll(resp.Body)
		var events []AgentEvent
		if err := json.Unmarshal(append(firstByte, rest...), &events); err != nil {
			return "", fmt.Errorf("decode streaming events array: %w", err)
		}
		for _, e := range events {
			if len(e.Content.Parts) > 0 {
				sb.WriteString(e.Content.Parts[0].Text)
			}
		}
	} else {
		multi := io.MultiReader(bytes.NewReader(firstByte[:n]), resp.Body)
		dec := json.NewDecoder(multi)
		for {
			var e AgentEvent
			if err := dec.Decode(&e); err == io.EOF {
				break
			} else if err != nil {
				slog.WarnContext(ctx, "Failed to decode streaming agent event", "error", err)
				break
			}
			if len(e.Content.Parts) > 0 {
				sb.WriteString(e.Content.Parts[0].Text)
			}
		}
	}

	return sb.String(), nil
}

func (r *AgentRunner) buildRunBody(appName, userID, sessionID, prompt string, stream bool) (io.Reader, error) {
	var req agentRunRequest
	req.AppName = appName
	req.UserID = userID
	req.SessionID = sessionID
	req.Stream = stream
	req.NewMessage.Role = "user"
	req.NewMessage.Parts = []struct {
		Text string `json:"text"`
	}{{Text: prompt}}

	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal run request: %w", err)
	}
	return bytes.NewReader(b), nil
}
