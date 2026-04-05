package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"app/models"
	"app/service"
)

// TestPerformDiscoveryMining_SessionFailure_DoesNotCallRun verifies that
// when the agent session creation returns a 5xx error, the /api/run endpoint
// is never called and the function returns cleanly.
func TestPerformDiscoveryMining_SessionFailure_DoesNotCallRun(t *testing.T) {
	var runCalled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/run" {
			runCalled.Store(true)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	h := &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    srv.URL,
		AgentClient: client,
		Agent:       &service.AgentRunner{Client: client, BaseURL: srv.URL},
		ResearchSem: make(chan struct{}, 10),
	}

	ctx := context.Background()
	h.performDiscoveryMining(ctx, 6) // June

	if runCalled.Load() {
		t.Error("expected /api/run to be skipped after session creation failure")
	}
}

// TestPerformDiscoveryMining_ContextCancellation verifies that a cancelled
// context causes the HTTP request to fail, and the function returns without
// calling /api/run.
func TestPerformDiscoveryMining_ContextCancellation(t *testing.T) {
	var sessionCalled, runCalled atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/run" {
			runCalled.Store(true)
		} else {
			sessionCalled.Store(true)
		}
		// Slow response to allow context cancellation to race.
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client2 := &http.Client{Timeout: 5 * time.Second}
	h := &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    srv.URL,
		AgentClient: client2,
		Agent:       &service.AgentRunner{Client: client2, BaseURL: srv.URL},
		ResearchSem: make(chan struct{}, 10),
	}

	// Cancel the context before the slow server responds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	h.performDiscoveryMining(ctx, 1)

	if runCalled.Load() {
		t.Error("expected /api/run not to be called when context is cancelled")
	}
}

// TestDiscoveryMining_AllMonths_HTTPTrigger verifies that triggering "all"
// months returns 202 Accepted immediately and launches background work.
func TestDiscoveryMining_AllMonths_HTTPTrigger(t *testing.T) {
	// Use a store stub that satisfies the interface for any DB calls the
	// mining loop might make (none in this test since the agent server
	// immediately returns 500, triggering early return each month).
	client3 := &http.Client{Timeout: 100 * time.Millisecond}
	h := &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    "http://127.0.0.1:0", // nothing listening — session creation fails fast
		AgentClient: client3,
		Agent:       &service.AgentRunner{Client: client3, BaseURL: "http://127.0.0.1:0"},
		ResearchSem: make(chan struct{}, 10),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/discovery/mine", h.DiscoveryMining)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/mine?month=all", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202 Accepted, got %d", w.Code)
	}
}

// TestDiscoveryMining_SingleMonth_HTTPTrigger verifies the single-month path.
func TestDiscoveryMining_SingleMonth_HTTPTrigger(t *testing.T) {
	client4 := &http.Client{Timeout: 100 * time.Millisecond}
	h := &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    "http://127.0.0.1:0",
		AgentClient: client4,
		Agent:       &service.AgentRunner{Client: client4, BaseURL: "http://127.0.0.1:0"},
		ResearchSem: make(chan struct{}, 10),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/discovery/mine", h.DiscoveryMining)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/mine?month=3", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202 Accepted, got %d", w.Code)
	}
}

// sessionTrackingStore stub for discovery tests — needs ListRegionsByMonth
// since performDiscoveryMining calls it after a successful agent run. The
// existing stub in agent_session_test.go doesn't implement it, but discovery
// mining only reaches that point on a successful agent response, which none
// of these tests exercise.
func (s *sessionTrackingStore) ListRegionsByMonth(_ context.Context, _ int) ([]models.RegionWithSeasonality, error) {
	return nil, nil
}
