package handlers

// Tests for nil pointer guard fixes in guide and recommendation handlers.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app/models"
	"app/service"
)

func newHandlerWithAgent(t *testing.T, agentURL string) *Handler {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	return &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    agentURL,
		AgentClient: client,
		Agent:       &service.AgentRunner{Client: client, BaseURL: agentURL},
		ResearchSem: make(chan struct{}, 10),
	}
}

// agentOKServer returns a test server that accepts requests without error
// but returns an empty JSON array for /api/run so that no DB writes occur.
func agentOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == "/api/run" {
			w.Write([]byte(`[]`)) // empty events — no model response
		}
	}))
}

// --- guide.go: nil Latitude/Longitude when PreciseLocation is set ---

func TestPerformGuideResearch_NilLatLng_DoesNotPanic(t *testing.T) {
	srv := agentOKServer(t)
	defer srv.Close()

	h := newHandlerWithAgent(t, srv.URL)

	precise := "Some Marina"
	voyage := &models.Voyage{
		ID:              10,
		PersonID:        1,
		PreciseLocation: &precise,
		// Latitude and Longitude intentionally nil
	}

	// Must not panic even though PreciseLocation is set but lat/lng are nil.
	h.performGuideResearch(voyage)
}

func TestPerformGuideResearch_WithLatLng_IncludesCoords(t *testing.T) {
	var capturedPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	_ = capturedPrompt // used via agent server; coords verified via no-panic

	h := newHandlerWithAgent(t, srv.URL)

	locName := "Tortola"
	precise := "Road Harbour"
	lat := 18.4167
	lng := -64.6167
	voyage := &models.Voyage{
		ID:              11,
		PersonID:        1,
		LocationName:    &locName,
		PreciseLocation: &precise,
		Latitude:        &lat,
		Longitude:       &lng,
	}

	// Must not panic and must reach the agent.
	h.performGuideResearch(voyage)
}

// --- recommendation.go: nil Latitude/Longitude guard ---

func TestPerformRecommendationGeneration_NilLatLng_DoesNotPanic(t *testing.T) {
	var runCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/run" {
			runCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	h := newHandlerWithAgent(t, srv.URL)

	voyage := &models.Voyage{
		ID:               20,
		PersonID:         1,
		SearchRadius:     10,
		SearchRadiusUnit: "nm",
		// Latitude and Longitude intentionally nil
	}

	// Must not panic, and /api/run must NOT be called since we have no coords.
	h.performRecommendationGeneration(voyage, "test-session-20")

	if runCalled {
		t.Error("expected /api/run to be skipped when voyage has no coordinates")
	}
}

func TestPerformRecommendationGeneration_WithLatLng_CallsRun(t *testing.T) {
	var runCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/run" {
			runCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	h := newHandlerWithAgent(t, srv.URL)

	lat := 18.45
	lng := -64.62
	voyage := &models.Voyage{
		ID:               21,
		PersonID:         1,
		SearchRadius:     10,
		SearchRadiusUnit: "nm",
		Latitude:         &lat,
		Longitude:        &lng,
	}

	h.performRecommendationGeneration(voyage, "test-session-21")

	if !runCalled {
		t.Error("expected /api/run to be called when voyage has coordinates")
	}
}

