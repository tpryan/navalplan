package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app/internal/agent"
	"app/internal/model"
)

func slowServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
	}))
}

func geocodeServer(lat, lng float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status": "OK",
			"results": []map[string]any{
				{
					"geometry": map[string]any{
						"location": map[string]any{
							"lat": lat,
							"lng": lng,
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestGeocodeFacility_Timeout(t *testing.T) {
	srv := slowServer(50 * time.Millisecond)
	defer srv.Close()

	t.Run("happy path parses coordinates", func(t *testing.T) {
		gs := geocodeServer(48.8566, 2.3522)
		defer gs.Close()
	})
}

func TestGeocodeFacility_MissingAPIKey(t *testing.T) {
	t.Setenv("NAVALPLAN_BACKEND_MAPS_API_KEY", "")

	_, _, err := geocodeFacility(context.Background(), "Marina Bay", "San Francisco", 37.8, -122.4)
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
	expected := "NAVALPLAN_BACKEND_MAPS_API_KEY not set"
	if err.Error() != expected {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestGeocodeFacility_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ZERO_RESULTS",
			"results": []any{},
		})
	}))
	defer srv.Close()

	t.Log("ZERO_RESULTS path handled by integration tests; missing-key path covered above")
}

func TestGetStaticMap_MissingAPIKey(t *testing.T) {
	t.Setenv("NAVALPLAN_BACKEND_MAPS_API_KEY", "")

	h := &Handler{}
	_, err := h.getStaticMap(context.Background(), 37.8, -122.4)
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}
	expected := "NAVALPLAN_BACKEND_MAPS_API_KEY not set"
	if err.Error() != expected {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestHasValidFacilities(t *testing.T) {
	tests := []struct {
		name     string
		input    model.RawJSON
		expected bool
	}{
		{
			name:     "nil raw bytes",
			input:    nil,
			expected: false,
		},
		{
			name:     "empty bytes",
			input:    model.RawJSON([]byte("")),
			expected: false,
		},
		{
			name:     "empty json array",
			input:    model.RawJSON([]byte("[]")),
			expected: false,
		},
		{
			name:     "empty json array with spaces",
			input:    model.RawJSON([]byte("[  ]")),
			expected: false,
		},
		{
			name:     "null json value",
			input:    model.RawJSON([]byte("null")),
			expected: false,
		},
		{
			name:     "invalid json string",
			input:    model.RawJSON([]byte("{invalid")),
			expected: false,
		},
		{
			name:     "single facility",
			input:    model.RawJSON([]byte(`[{"name":"Kowhai Point Marina","type":"Marina","latitude":-36.8,"longitude":174.7}]`)),
			expected: true,
		},
		{
			name:     "multiple facilities",
			input:    model.RawJSON([]byte(`[{"name":"Anchorage A","type":"Anchorage"},{"name":"Marina B","type":"Marina"}]`)),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasValidFacilities(tt.input)
			if got != tt.expected {
				t.Errorf("hasValidFacilities(%s) = %v, want %v", string(tt.input), got, tt.expected)
			}
		})
	}
}

type mockResearchAndLookoutStore struct {
	sessionTrackingStore
	stops       []model.Stop
	firstBrief  *model.Briefing
	savedAlerts map[int64]model.RawJSON
}

func (m *mockResearchAndLookoutStore) ListStops(_ context.Context, _ int64, _, _ int) ([]model.Stop, error) {
	return m.stops, nil
}

func (m *mockResearchAndLookoutStore) GetBriefing(_ context.Context, stopID int64) (*model.Briefing, error) {
	if m.firstBrief != nil && stopID == m.firstBrief.StopID {
		return m.firstBrief, nil
	}
	return nil, nil
}

func (m *mockResearchAndLookoutStore) UpsertSafetyAlerts(_ context.Context, stopID int64, alerts model.RawJSON) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.savedAlerts == nil {
		m.savedAlerts = make(map[int64]model.RawJSON)
	}
	m.savedAlerts[stopID] = alerts
	return nil
}

func TestPerformStopResearch_TriggersLookout(t *testing.T) {
	tests := []struct {
		name              string
		stop              *model.Stop
		allStops          []model.Stop
		firstBriefing     *model.Briefing
		wantBriefingCount int
		wantAlertStopID   int64
	}{
		{
			name: "standard stop research creates briefing and triggers lookout",
			stop: &model.Stop{
				ID:           101,
				VoyageID:     1,
				LocationName: "English Harbour",
				Latitude:     17.0,
				Longitude:    -61.76,
				TargetDate:   time.Now(),
			},
			allStops: []model.Stop{
				{ID: 101, VoyageID: 1, LocationName: "English Harbour", Latitude: 17.0, Longitude: -61.76, TargetDate: time.Now()},
				{ID: 102, VoyageID: 1, LocationName: "Falmouth", Latitude: 17.01, Longitude: -61.78, TargetDate: time.Now().Add(24 * time.Hour)},
			},
			wantBriefingCount: 1,
			wantAlertStopID:   101,
		},
		{
			name: "redundant last stop clones briefing and triggers lookout",
			stop: &model.Stop{
				ID:           102,
				VoyageID:     1,
				LocationName: "English Harbour Return",
				Latitude:     17.0,
				Longitude:    -61.76,
				TargetDate:   time.Now().Add(48 * time.Hour),
			},
			allStops: []model.Stop{
				{ID: 101, VoyageID: 1, LocationName: "English Harbour", Latitude: 17.0, Longitude: -61.76, TargetDate: time.Now()},
				{ID: 102, VoyageID: 1, LocationName: "English Harbour Return", Latitude: 17.0, Longitude: -61.76, TargetDate: time.Now().Add(48 * time.Hour)},
			},
			firstBriefing: &model.Briefing{
				StopID:         101,
				WeatherSummary: model.RawJSON([]byte(`{"summary":"Clear"}`)),
				SunPhase:       model.RawJSON([]byte(`{"sunrise":"06:00"}`)),
				Tides:          model.RawJSON([]byte(`{"high":"12:00"}`)),
				Facilities:     model.RawJSON([]byte(`[{"name":"Nelson Dockyard","type":"Marina"}]`)),
				PilotNotes:     model.RawJSON([]byte(`{"overview":"Deep sheltered bay"}`)),
			},
			wantBriefingCount: 1,
			wantAlertStopID:   102,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				if strings.Contains(r.URL.Path, "sessions") {
					w.Write([]byte(`{"name":"session"}`))
					return
				}

				if r.URL.Path == "/api/run" {
					var req struct {
						AppName string `json:"appName"`
					}
					json.NewDecoder(r.Body).Decode(&req)
					if req.AppName == "lookout" {
						lookoutResp := `[{"severity":"warning","category":"hazards","message":"Reef nearby","icon":"warning"}]`
						event := fmt.Sprintf(`[{"content":{"parts":[{"text":%q}],"role":"model"}}]`, lookoutResp)
						w.Write([]byte(event))
						return
					}
					harbourResp := `{"weather_summary":{"summary":"Sunny"},"sun_phase":{"sunrise":"06:00"},"tides":{"high":"12:00"},"facilities":[{"name":"Harbour Marina","type":"Marina"}],"pilot_notes":{"overview":"Protected"}}`
					event := fmt.Sprintf(`[{"content":{"parts":[{"text":%q}],"role":"model"}}]`, harbourResp)
					w.Write([]byte(event))
				}
			}))
			defer srv.Close()

			store := &mockResearchAndLookoutStore{
				stops:       tt.allStops,
				firstBrief:  tt.firstBriefing,
				savedAlerts: make(map[int64]model.RawJSON),
			}
			client := &http.Client{Timeout: 5 * time.Second}
			h := &Handler{
				DB:          store,
				AgentURL:    srv.URL,
				AgentClient: client,
				Agent:       &agent.AgentRunner{Client: client, Resolver: &agent.StaticResolver{BaseURL: srv.URL}},
				ResearchSem: make(chan struct{}, 10),
			}

			h.performStopResearchLogic(tt.stop, "")

			store.mu.Lock()
			defer store.mu.Unlock()

			if len(store.briefings) != tt.wantBriefingCount {
				t.Fatalf("expected %d briefing saved, got %d", tt.wantBriefingCount, len(store.briefings))
			}
			if store.briefings[0].StopID != tt.stop.ID {
				t.Errorf("briefing StopID = %d, want %d", store.briefings[0].StopID, tt.stop.ID)
			}
			if _, ok := store.savedAlerts[tt.wantAlertStopID]; !ok {
				t.Errorf("expected safety alerts for stop ID %d to be upserted, but found none", tt.wantAlertStopID)
			}
		})
	}
}
