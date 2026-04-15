package handlers

// Tests for early-return behaviour when agent session creation fails.
// These are internal tests (package handlers) so they can access unexported methods.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"app/datastore"
	"app/models"
	"app/service"
)

// sessionTrackingStore records CreateBriefing and CreateVoyageGuide calls so
// tests can verify the save-empty paths are reached without a real database.
// All other Store methods panic — if an unexpected call reaches them the test
// fails loudly.
type sessionTrackingStore struct {
	datastore.Store // unimplemented methods panic intentionally

	mu         sync.Mutex
	briefings  []*models.Briefing
	guides     []*models.VoyageGuide
}

func (s *sessionTrackingStore) ListStops(_ context.Context, _ int64, _, _ int) ([]models.Stop, error) {
	return nil, nil
}

func (s *sessionTrackingStore) GetBriefing(_ context.Context, _ int64) (*models.Briefing, error) {
	return nil, nil
}

func (s *sessionTrackingStore) GetNearbyBriefing(_ context.Context, _, _ float64) (*models.Briefing, error) {
	return nil, nil
}

func (s *sessionTrackingStore) CreateBriefing(_ context.Context, b *models.Briefing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.briefings = append(s.briefings, b)
	return nil
}

func (s *sessionTrackingStore) CreateVoyageGuide(_ context.Context, g *models.VoyageGuide) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guides = append(s.guides, g)
	return nil
}

func (s *sessionTrackingStore) SaveVoyageMap(_ context.Context, _ int64, _ []byte) error {
	return nil
}

func (s *sessionTrackingStore) DeleteVoyageRecommendations(_ context.Context, _ int64) error {
	return nil
}

// agentServerWithSessionFailure starts a test HTTP server whose session
// creation endpoint returns 500 and records whether /api/run was called.
func agentServerWithSessionFailure(t *testing.T) (srv *httptest.Server, runCalled *bool) {
	t.Helper()
	called := false
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/run" {
			called = true
		}
		// Return 500 for everything — session creation POST will fail.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	return srv, &called
}

func TestPerformStopResearch_SessionFailure_SavesEmptyBriefing(t *testing.T) {
	agentSrv, runCalled := agentServerWithSessionFailure(t)
	defer agentSrv.Close()

	store := &sessionTrackingStore{}
	client := &http.Client{Timeout: 5 * time.Second}
	h := &Handler{
		DB:          store,
		AgentURL:    agentSrv.URL,
		AgentClient: client,
		Agent:       &service.AgentRunner{Client: client, BaseURL: agentSrv.URL},
		ResearchSem: make(chan struct{}, 10),
	}

	stop := &models.Stop{
		ID:               1,
		LocationName:     "Test Anchorage",
		Latitude:         18.45,
		Longitude:        -64.62,
		TargetDate:       time.Now(),
		SearchRadius:     5,
		SearchRadiusUnit: "nm",
	}

	h.performStopResearchLogic(stop, "")

	// Session creation failed → /api/run must NOT have been called.
	if *runCalled {
		t.Error("expected /api/run to be skipped after session creation failure")
	}

	// An empty briefing must have been saved so the stop has a record.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.briefings) != 1 {
		t.Fatalf("expected 1 briefing saved, got %d", len(store.briefings))
	}
	if store.briefings[0].StopID != stop.ID {
		t.Errorf("briefing StopID = %d, want %d", store.briefings[0].StopID, stop.ID)
	}
}

func TestPerformGuideResearch_SessionFailure_SavesEmptyGuide(t *testing.T) {
	agentSrv, runCalled := agentServerWithSessionFailure(t)
	defer agentSrv.Close()

	store := &sessionTrackingStore{}
	client2 := &http.Client{Timeout: 5 * time.Second}
	h := &Handler{
		DB:          store,
		AgentURL:    agentSrv.URL,
		AgentClient: client2,
		Agent:       &service.AgentRunner{Client: client2, BaseURL: agentSrv.URL},
		ResearchSem: make(chan struct{}, 10),
	}

	locName := "British Virgin Islands"
	voyage := &models.Voyage{
		ID:           42,
		PersonID:     1,
		LocationName: &locName,
	}

	h.performGuideResearch(voyage, "")

	if *runCalled {
		t.Error("expected /api/run to be skipped after session creation failure")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.guides) != 1 {
		t.Fatalf("expected 1 guide saved, got %d", len(store.guides))
	}
	if store.guides[0].VoyageID != voyage.ID {
		t.Errorf("guide VoyageID = %d, want %d", store.guides[0].VoyageID, voyage.ID)
	}
}

func TestPerformRecommendation_SessionFailure_DoesNotCallRun(t *testing.T) {
	agentSrv, runCalled := agentServerWithSessionFailure(t)
	defer agentSrv.Close()

	store := &sessionTrackingStore{}
	client3 := &http.Client{Timeout: 5 * time.Second}
	h := &Handler{
		DB:          store,
		AgentURL:    agentSrv.URL,
		AgentClient: client3,
		Agent:       &service.AgentRunner{Client: client3, BaseURL: agentSrv.URL},
		ResearchSem: make(chan struct{}, 10),
	}

	lat := 18.45
	lng := -64.62
	voyage := &models.Voyage{
		ID:               99,
		PersonID:         1,
		Latitude:         &lat,
		Longitude:        &lng,
		SearchRadius:     10,
		SearchRadiusUnit: "nm",
	}

	h.performRecommendationGeneration(voyage, "test-session-99", "")

	if *runCalled {
		t.Error("expected /api/run to be skipped after session creation failure")
	}
}
