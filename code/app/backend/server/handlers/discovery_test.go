package handlers

import (
	"context"
	"encoding/json"
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
		Agent:       &service.AgentRunner{Client: client, Resolver: &service.StaticResolver{BaseURL: srv.URL}},
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
		Agent:       &service.AgentRunner{Client: client2, Resolver: &service.StaticResolver{BaseURL: srv.URL}},
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
		Agent:       &service.AgentRunner{Client: client3, Resolver: &service.StaticResolver{BaseURL: "http://127.0.0.1:0"}},
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
		Agent:       &service.AgentRunner{Client: client4, Resolver: &service.StaticResolver{BaseURL: "http://127.0.0.1:0"}},
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

func TestIsSpatialDuplicate(t *testing.T) {
	tests := []struct {
		name        string
		iou         float64
		containment float64
		want        bool
	}{
		{"Low overlap", 0.1, 0.2, false},
		{"High IoU", 0.5, 0.5, true},
		{"High Containment sub-region", 0.2, 0.8, true},
		{"Borderline IoU below threshold", 0.35, 0.65, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSpatialDuplicate(tt.iou, tt.containment)
			if got != tt.want {
				t.Errorf("isSpatialDuplicate(%v, %v) = %v; want %v", tt.iou, tt.containment, got, tt.want)
			}
		})
	}
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
	regions       map[int][]models.RegionWithSeasonality
	deletedRegion map[int]int
}

func (p *pruneMockStore) ListRegionsByMonth(_ context.Context, month int) ([]models.RegionWithSeasonality, error) {
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
		regions: map[int][]models.RegionWithSeasonality{
			7: {
				{
					SailingRegion:    models.SailingRegion{ID: 1, Name: "Society Islands, French Polynesia", Geometry: models.RawJSON(geomA)},
					SuitabilityScore: 95,
					Tier:             "Standard",
					IsHiddenGem:      false,
				},
				{
					SailingRegion:    models.SailingRegion{ID: 2, Name: "French Polynesia (Leeward Islands)", Geometry: models.RawJSON(geomB)},
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
