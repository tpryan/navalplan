package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"app/internal/agent"
	"app/internal/model"
)

type sessionTrackingStore struct {
	DBStore

	mu        sync.Mutex
	briefings []*model.Briefing
	guides    []*model.VoyageGuide
}

func (s *sessionTrackingStore) ListStops(_ context.Context, _ int64, _, _ int) ([]model.Stop, error) {
	return nil, nil
}

func (s *sessionTrackingStore) GetBriefing(_ context.Context, _ int64) (*model.Briefing, error) {
	return nil, nil
}

func (s *sessionTrackingStore) GetNearbyBriefing(_ context.Context, _, _ float64) (*model.Briefing, error) {
	return nil, nil
}

func (s *sessionTrackingStore) CreateBriefing(_ context.Context, b *model.Briefing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.briefings = append(s.briefings, b)
	return nil
}

func (s *sessionTrackingStore) CreateVoyageGuide(_ context.Context, g *model.VoyageGuide) error {
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

func (s *sessionTrackingStore) GetVoyageGuide(_ context.Context, _ int64) (*model.VoyageGuide, error) {
	return nil, nil
}

func (s *sessionTrackingStore) UpsertSafetyAlerts(_ context.Context, _ int64, _ model.RawJSON) error {
	return nil
}

func agentServerWithSessionFailure(t *testing.T) (srv *httptest.Server, runCalled *bool) {
	t.Helper()
	called := false
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/run" {
			called = true
		}
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
		Agent:       &agent.AgentRunner{Client: client, Resolver: &agent.StaticResolver{BaseURL: agentSrv.URL}},
		ResearchSem: make(chan struct{}, 10),
	}

	stop := &model.Stop{
		ID:               1,
		LocationName:     "Test Anchorage",
		Latitude:         18.45,
		Longitude:        -64.62,
		TargetDate:       time.Now(),
		SearchRadius:     5,
		SearchRadiusUnit: "nm",
	}

	h.performStopResearchLogic(stop, "")

	if *runCalled {
		t.Error("expected /api/run to be skipped after session creation failure")
	}

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
		Agent:       &agent.AgentRunner{Client: client2, Resolver: &agent.StaticResolver{BaseURL: agentSrv.URL}},
		ResearchSem: make(chan struct{}, 10),
	}

	locName := "British Virgin Islands"
	voyage := &model.Voyage{
		ID:           42,
		PersonID:     1,
		LocationName: &locName,
	}

	h.performGuideResearch(voyage, "", "")

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
		Agent:       &agent.AgentRunner{Client: client3, Resolver: &agent.StaticResolver{BaseURL: agentSrv.URL}},
		ResearchSem: make(chan struct{}, 10),
	}

	lat := 18.45
	lng := -64.62
	voyage := &model.Voyage{
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
