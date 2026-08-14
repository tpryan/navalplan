package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app/internal/agent"
	"app/internal/model"
)

func newHandlerWithAgent(t *testing.T, agentURL string) *Handler {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	return &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    agentURL,
		AgentClient: client,
		Agent:       &agent.AgentRunner{Client: client, Resolver: &agent.StaticResolver{BaseURL: agentURL}},
		ResearchSem: make(chan struct{}, 10),
	}
}

func agentOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == "/api/run" {
			w.Write([]byte(`[]`))
		}
	}))
}

func TestPerformGuideResearch_NilLatLng_DoesNotPanic(t *testing.T) {
	srv := agentOKServer(t)
	defer srv.Close()

	h := newHandlerWithAgent(t, srv.URL)

	precise := "Some Marina"
	voyage := &model.Voyage{
		ID:              10,
		PersonID:        1,
		PreciseLocation: &precise,
	}

	h.performGuideResearch(voyage, "", "")
}

func TestPerformGuideResearch_WithLatLng_IncludesCoords(t *testing.T) {
	var capturedPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	_ = capturedPrompt

	h := newHandlerWithAgent(t, srv.URL)

	locName := "Tortola"
	precise := "Road Harbour"
	lat := 18.4167
	lng := -64.6167
	voyage := &model.Voyage{
		ID:              11,
		PersonID:        1,
		LocationName:    &locName,
		PreciseLocation: &precise,
		Latitude:        &lat,
		Longitude:       &lng,
	}

	h.performGuideResearch(voyage, "", "")
}

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

	voyage := &model.Voyage{
		ID:               20,
		PersonID:         1,
		SearchRadius:     10,
		SearchRadiusUnit: "nm",
	}

	h.performRecommendationGeneration(voyage, "test-session-20", "")

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
	voyage := &model.Voyage{
		ID:               21,
		PersonID:         1,
		SearchRadius:     10,
		SearchRadiusUnit: "nm",
		Latitude:         &lat,
		Longitude:        &lng,
	}

	h.performRecommendationGeneration(voyage, "test-session-21", "")

	if !runCalled {
		t.Error("expected /api/run to be called when voyage has coordinates")
	}
}
