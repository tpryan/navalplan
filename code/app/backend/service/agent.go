package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/idtoken"
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
	NewMessage struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"newMessage"`
}

// AgentEvent mirrors the ADK event structure.
type AgentEvent struct {
	Content struct {
		Parts []struct {
			Text    string `json:"text"`
			Thought bool   `json:"thought,omitempty"`
		} `json:"parts"`
		Role string `json:"role"`
	} `json:"content"`
}

// CreateSession creates an agent session. state is optional; pass nil for no
// session state.
func (r *AgentRunner) CreateSession(ctx context.Context, appName, userID, sessionID string, state map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

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
		payload := map[string]any{"state": state}
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal session state: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("build session request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if strings.Contains(target, "run.app") {
		r.addAuthHeader(req, target)
	}

	resp, err := r.Client.Do(req)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("create session returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// RunSync calls the agent and returns concatenated text from
// all model-role events. It uses streaming under the hood to ensure continuous socket activity.
func (r *AgentRunner) RunSync(ctx context.Context, appName, userID, sessionID, prompt string) (string, error) {
	target, err := r.getTarget(ctx, appName)
	if err != nil {
		return "", err
	}

	if r.isReasoningEngine(target) && r.ReasoningEngine != nil {
		slog.InfoContext(ctx, "invoking agent via Reasoning Engine", "target", target, "appName", appName)
		return r.ReasoningEngine.RunSync(ctx, target, appName, userID, sessionID, prompt)
	}

	// Default to RunStreaming for HTTP targets as well
	return r.RunStreaming(ctx, appName, userID, sessionID, prompt)
}

// RunStreaming calls the agent with streaming enabled and returns concatenated
// text from all event parts. It handles SSE (data: ...), NDJSON, and JSON-array
// response formats.
func (r *AgentRunner) RunStreaming(ctx context.Context, appName, userID, sessionID, prompt string) (string, error) {
	target, err := r.getTarget(ctx, appName)
	if err != nil {
		return "", err
	}

	if r.isReasoningEngine(target) && r.ReasoningEngine != nil {
		slog.InfoContext(ctx, "invoking agent via Reasoning Engine", "target", target, "appName", appName)
		return r.ReasoningEngine.RunStreaming(ctx, target, appName, userID, sessionID, prompt)
	}

	slog.InfoContext(ctx, "invoking agent via HTTP", "target", target, "appName", appName)

	body, err := r.buildRunBody(appName, userID, sessionID, prompt, true)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target+"/api/run", body)
	if err != nil {
		return "", fmt.Errorf("build run request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.Contains(target, "run.app") {
		r.addAuthHeader(req, target)
	}

	resp, err := r.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("run agent (streaming): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(b))
	}

	// Peek at the first byte to detect format (JSON array vs NDJSON / SSE).
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
			if (e.Content.Role == "" || e.Content.Role == "model") && len(e.Content.Parts) > 0 {
				for _, p := range e.Content.Parts {
					if p.Text != "" && !p.Thought {
						sb.WriteString(p.Text)
					}
				}
			}
		}
	} else {
		multi := io.MultiReader(bytes.NewReader(firstByte[:n]), resp.Body)
		reader := bufio.NewReader(multi)
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil && len(line) == 0 {
				if readErr == io.EOF {
					break
				}
				slog.WarnContext(ctx, "Failed to read streaming agent event line", "error", readErr)
				break
			}
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
				if readErr == io.EOF {
					break
				}
				continue
			}

			if strings.HasPrefix(line, "data:") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if line == "" || line == "[DONE]" {
					if readErr == io.EOF {
						break
					}
					continue
				}
			}

			var e AgentEvent
			if decodeErr := json.Unmarshal([]byte(line), &e); decodeErr != nil {
				slog.WarnContext(ctx, "Failed to decode streaming agent event", "error", decodeErr, "line", line)
				if readErr == io.EOF {
					break
				}
				continue
			}

			if (e.Content.Role == "" || e.Content.Role == "model") && len(e.Content.Parts) > 0 {
				for _, p := range e.Content.Parts {
					if p.Text != "" && !p.Thought {
						sb.WriteString(p.Text)
					}
				}
			}

			if readErr == io.EOF {
				break
			}
		}
	}

	return sb.String(), nil
}

func (r *AgentRunner) addAuthHeader(req *http.Request, audience string) {
	// The audience should be the base service URL for Cloud Run
	tokenSource, err := idtoken.NewTokenSource(context.Background(), audience)
	if err != nil {
		slog.Error("Failed to create ID token source", "error", err)
		return
	}
	token, err := tokenSource.Token()
	if err != nil {
		slog.Error("Failed to get ID token", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
}

func (r *AgentRunner) buildRunBody(appName, userID, sessionID, prompt string, stream bool) (io.Reader, error) {
	var req agentRunRequest
	req.AppName = appName
	req.UserID = userID
	req.SessionID = sessionID
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
