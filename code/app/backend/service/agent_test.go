package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"app/service"
)

// agentEvent mirrors service.AgentEvent for building test responses.
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

func newRunner(baseURL string) *service.AgentRunner {
	return &service.AgentRunner{
		Client:   &http.Client{},
		Resolver: &service.StaticResolver{BaseURL: baseURL},
	}
}

// TestCreateSession_Success verifies that a 200 session creation succeeds.
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

// TestCreateSession_ServerError verifies that a 500 from session creation returns an error.
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

// TestCreateSession_WithState verifies that session state is sent as JSON.
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

// TestRunSync_ReturnsModelText verifies that RunSync extracts text from model-role events.
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

// TestRunSync_NonOKStatus verifies that a non-200 agent response returns an error.
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

// TestRunStreaming_JSONArray verifies that RunStreaming handles a JSON array response.
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

// TestRunStreaming_NDJSON verifies that RunStreaming handles NDJSON-formatted responses.
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

// TestRunStreaming_SSE verifies that RunStreaming handles Server-Sent Events (SSE) responses from adkrest.
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
