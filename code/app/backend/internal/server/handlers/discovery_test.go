package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"app/internal/agent"
	"app/internal/model"
)

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
		Agent:       &agent.AgentRunner{Client: client, Resolver: &agent.StaticResolver{BaseURL: srv.URL}},
		ResearchSem: make(chan struct{}, 10),
	}

	ctx := context.Background()
	h.performDiscoveryMining(ctx, 6)

	if runCalled.Load() {
		t.Error("expected /api/run to be skipped after session creation failure")
	}
}

func TestPerformDiscoveryMining_ContextCancellation(t *testing.T) {
	var sessionCalled, runCalled atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/run" {
			runCalled.Store(true)
		} else {
			sessionCalled.Store(true)
		}
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client2 := &http.Client{Timeout: 5 * time.Second}
	h := &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    srv.URL,
		AgentClient: client2,
		Agent:       &agent.AgentRunner{Client: client2, Resolver: &agent.StaticResolver{BaseURL: srv.URL}},
		ResearchSem: make(chan struct{}, 10),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	h.performDiscoveryMining(ctx, 1)

	if runCalled.Load() {
		t.Error("expected /api/run not to be called when context is cancelled")
	}
}

func TestDiscoveryMining_AllMonths_HTTPTrigger(t *testing.T) {
	client3 := &http.Client{Timeout: 100 * time.Millisecond}
	h := &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    "http://127.0.0.1:0",
		AgentClient: client3,
		Agent:       &agent.AgentRunner{Client: client3, Resolver: &agent.StaticResolver{BaseURL: "http://127.0.0.1:0"}},
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

func TestDiscoveryMining_SingleMonth_HTTPTrigger(t *testing.T) {
	client4 := &http.Client{Timeout: 100 * time.Millisecond}
	h := &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    "http://127.0.0.1:0",
		AgentClient: client4,
		Agent:       &agent.AgentRunner{Client: client4, Resolver: &agent.StaticResolver{BaseURL: "http://127.0.0.1:0"}},
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

func (s *sessionTrackingStore) ListRegionsByMonth(_ context.Context, _ int) ([]model.RegionWithSeasonality, error) {
	return nil, nil
}

func TestDiscoveryPruning_HTTPTrigger(t *testing.T) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	h := &Handler{
		DB:          &sessionTrackingStore{},
		AgentURL:    "http://127.0.0.1:0",
		AgentClient: client,
		ResearchSem: make(chan struct{}, 10),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/discovery/prune", h.DiscoveryPruning)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/discovery/prune?month=7", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", w.Code)
	}
}

type pruneMockStore struct {
	sessionTrackingStore
	regions       map[int][]model.RegionWithSeasonality
	deletedRegion map[int]int
}

func (p *pruneMockStore) ListRegionsByMonth(_ context.Context, month int) ([]model.RegionWithSeasonality, error) {
	return p.regions[month], nil
}

func (p *pruneMockStore) DeleteSeasonality(_ context.Context, regionID int, month int) error {
	if p.deletedRegion == nil {
		p.deletedRegion = make(map[int]int)
	}
	p.deletedRegion[regionID] = month
	return nil
}

func TestPruneDuplicateRegions(t *testing.T) {
	geomA := json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]]]}`)
	geomB := json.RawMessage(`{"type":"Polygon","coordinates":[[[1,1],[9,1],[9,9],[1,9],[1,1]]]}`)

	store := &pruneMockStore{
		regions: map[int][]model.RegionWithSeasonality{
			7: {
				{
					SailingRegion:    model.SailingRegion{ID: 1, Name: "Society Islands, French Polynesia", Geometry: model.RawJSON(geomA)},
					SuitabilityScore: 95,
					Tier:             "Standard",
					IsHiddenGem:      false,
				},
				{
					SailingRegion:    model.SailingRegion{ID: 2, Name: "French Polynesia (Leeward Islands)", Geometry: model.RawJSON(geomB)},
					SuitabilityScore: 96,
					Tier:             "Standard",
					IsHiddenGem:      false,
				},
			},
		},
	}

	h := &Handler{DB: store}
	pruned, err := h.PruneDuplicateRegions(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error during pruning: %v", err)
	}

	if pruned != 1 {
		t.Errorf("expected 1 region pruned, got %d", pruned)
	}

	if _, ok := store.deletedRegion[1]; !ok {
		t.Errorf("expected region 1 to be deleted during pruning")
	}
}

func TestPruneDuplicateRegions_StackedSocietyIslands(t *testing.T) {
	// Two adjacent/partially-overlapping Society Islands boxes (~1.2° apart, IoU ~0.20)
	geomNorth := json.RawMessage(`{"type":"Polygon","coordinates":[[[-152.5,-16.0],[-149.0,-16.0],[-149.0,-17.3],[-152.5,-17.3],[-152.5,-16.0]]]}`)
	geomSouth := json.RawMessage(`{"type":"Polygon","coordinates":[[[-152.5,-16.9],[-149.0,-16.9],[-149.0,-18.2],[-152.5,-18.2],[-152.5,-16.9]]]}`)

	store := &pruneMockStore{
		regions: map[int][]model.RegionWithSeasonality{
			6: {
				{
					SailingRegion:    model.SailingRegion{ID: 10, Name: "Society Islands", Geometry: model.RawJSON(geomNorth)},
					SuitabilityScore: 92,
					Tier:             "Standard",
				},
				{
					SailingRegion:    model.SailingRegion{ID: 11, Name: "Tahiti & Society Islands", Geometry: model.RawJSON(geomSouth)},
					SuitabilityScore: 88,
					Tier:             "Standard",
				},
			},
		},
	}

	h := &Handler{DB: store}
	pruned, err := h.PruneDuplicateRegions(context.Background(), 6)
	if err != nil {
		t.Fatalf("unexpected error during pruning: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 stacked Society Islands region pruned, got %d", pruned)
	}
	if _, ok := store.deletedRegion[11]; !ok {
		t.Errorf("expected lower-score region 11 to be deleted")
	}
}

func TestDeleteDiscoveryRegions_AllMonths(t *testing.T) {
	store := &pruneMockStore{
		regions: map[int][]model.RegionWithSeasonality{
			1: {{SailingRegion: model.SailingRegion{ID: 1, Name: "Caribbean"}}},
			7: {{SailingRegion: model.SailingRegion{ID: 2, Name: "Mediterranean"}}},
		},
	}

	h := &Handler{DB: store}
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/discovery/regions", h.DeleteDiscoveryRegions)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/discovery/regions?month=all", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if int(resp["deleted_count"].(float64)) != 2 {
		t.Errorf("expected deleted_count=2, got %v", resp["deleted_count"])
	}
	if store.deletedRegion[1] != 1 || store.deletedRegion[2] != 7 {
		t.Errorf("unexpected deletedRegion map: %v", store.deletedRegion)
	}
}
