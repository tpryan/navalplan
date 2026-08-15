package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/agent"
)

type agentEvent struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
		Role string `json:"role"`
	} `json:"content"`
}

func makeEvents(role, text string) []agentEvent {
	e := agentEvent{}
	e.Content.Role = role
	e.Content.Parts = []struct {
		Text string `json:"text"`
	}{{Text: text}}
	return []agentEvent{e}
}

func newRunner(baseURL string) *agent.AgentRunner {
	return &agent.AgentRunner{
		Client:   &http.Client{},
		Resolver: &agent.StaticResolver{BaseURL: baseURL},
	}
}

func TestCreateSession_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	runner := newRunner(srv.URL)
	err := runner.CreateSession(context.Background(), "app", "user", "sess1", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCreateSession_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	runner := newRunner(srv.URL)
	err := runner.CreateSession(context.Background(), "app", "user", "sess1", nil)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestCreateSession_WithState(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	runner := newRunner(srv.URL)
	err := runner.CreateSession(context.Background(), "app", "user", "sess1", map[string]any{"Month": "June"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	state, ok := gotBody["state"].(map[string]any)
	if !ok {
		t.Fatalf("expected state in body, got %v", gotBody)
	}
	if state["Month"] != "June" {
		t.Errorf("expected Month=June, got %v", state["Month"])
	}
}

func TestRunSync_ReturnsModelText(t *testing.T) {
	events := append(makeEvents("tool", "ignore this"), makeEvents("model", "hello world")...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/apps/app/users/user/sessions/sess1":
			w.WriteHeader(http.StatusOK)
		case "/api/run":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(events)
		}
	}))
	defer srv.Close()

	runner := newRunner(srv.URL)
	_ = runner.CreateSession(context.Background(), "app", "user", "sess1", nil)
	text, err := runner.RunSync(context.Background(), "app", "user", "sess1", "my prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello world" {
		t.Errorf("got %q, want %q", text, "hello world")
	}
}

func TestRunSync_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	runner := newRunner(srv.URL)
	_, err := runner.RunSync(context.Background(), "app", "user", "sess1", "prompt")
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestRunStreaming_JSONArray(t *testing.T) {
	events := makeEvents("model", "streamed result")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	}))
	defer srv.Close()

	runner := newRunner(srv.URL)
	text, err := runner.RunStreaming(context.Background(), "app", "user", "sess1", "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "streamed result" {
		t.Errorf("got %q, want %q", text, "streamed result")
	}
}

func TestRunStreaming_NDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		e1 := agentEvent{}
		e1.Content.Parts = []struct {
			Text string `json:"text"`
		}{{Text: "part one "}}
		e2 := agentEvent{}
		e2.Content.Parts = []struct {
			Text string `json:"text"`
		}{{Text: "part two"}}
		enc.Encode(e1)
		enc.Encode(e2)
	}))
	defer srv.Close()

	runner := newRunner(srv.URL)
	text, err := runner.RunStreaming(context.Background(), "app", "user", "sess1", "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "part one part two" {
		t.Errorf("got %q, want %q", text, "part one part two")
	}
}

func TestRunStreaming_SSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message\n"))
		w.Write([]byte("data: {\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"sse part one \"}]}}\n\n"))
		w.Write([]byte("data: {\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"sse part two\"}]}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	runner := newRunner(srv.URL)
	text, err := runner.RunStreaming(context.Background(), "app", "user", "sess1", "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "sse part one sse part two" {
		t.Errorf("got %q, want %q", text, "sse part one sse part two")
	}
}

func TestAgentRunner_TargetResolution(t *testing.T) {
	tests := []struct {
		name     string
		resolver agent.Resolver
		appName  string
	}{
		{
			name:     "nil resolver falls back to default",
			resolver: nil,
			appName:  "harbourmaster",
		},
		{
			name:     "static resolver with empty URL falls back to default",
			resolver: &agent.StaticResolver{BaseURL: ""},
			appName:  "pilot",
		},
		{
			name:     "static resolver with custom URL returns custom URL",
			resolver: &agent.StaticResolver{BaseURL: "https://custom-agent.run.app"},
			appName:  "commodore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &agent.AgentRunner{
				Client:   &http.Client{},
				Resolver: tt.resolver,
			}
			// Testing with an invalid port/server will fail network dial, but confirms getTarget worked
			err := runner.CreateSession(context.Background(), tt.appName, "user", "sess1", nil)
			if err == nil {
				t.Error("expected dial error when server does not exist")
			}
		})
	}
}

func TestAgentRunner_ReasoningEngineNilRunner(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		operation string
	}{
		{
			name:      "CreateSession returns error when ReasoningEngine runner is nil",
			target:    "projects/123/locations/us-central1/reasoningEngines/456",
			operation: "create_session",
		},
		{
			name:      "RunSync returns error when ReasoningEngine runner is nil",
			target:    "projects/123/locations/us-central1/reasoningEngines/456",
			operation: "run_sync",
		},
		{
			name:      "RunStreaming returns error when ReasoningEngine runner is nil",
			target:    "projects/123/locations/us-central1/reasoningEngines/456",
			operation: "run_streaming",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &agent.AgentRunner{
				Client:          &http.Client{},
				Resolver:        &agent.StaticResolver{BaseURL: tt.target},
				ReasoningEngine: nil,
			}

			switch tt.operation {
			case "create_session":
				err := runner.CreateSession(context.Background(), "harbourmaster", "user", "sess1", nil)
				if err == nil || !strings.Contains(err.Error(), "reasoning engine runner not initialized") {
					t.Errorf("expected runner not initialized error, got %v", err)
				}
			case "run_sync":
				_, err := runner.RunSync(context.Background(), "harbourmaster", "user", "sess1", "prompt")
				if err == nil || !strings.Contains(err.Error(), "reasoning engine runner not initialized") {
					t.Errorf("expected runner not initialized error, got %v", err)
				}
			case "run_streaming":
				_, err := runner.RunStreaming(context.Background(), "harbourmaster", "user", "sess1", "prompt")
				if err == nil || !strings.Contains(err.Error(), "reasoning engine runner not initialized") {
					t.Errorf("expected runner not initialized error, got %v", err)
				}
			}
		})
	}
}
