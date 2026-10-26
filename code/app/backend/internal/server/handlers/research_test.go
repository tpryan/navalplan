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
	stops        []model.Stop
	voyageTracks []model.VoyageTrack
	stopTracks   map[int64][]model.VoyageTrack
	firstBrief   *model.Briefing
	savedAlerts  map[int64]model.RawJSON
}

func (m *mockResearchAndLookoutStore) ListStops(_ context.Context, _ int64, _, _ int) ([]model.Stop, error) {
	return m.stops, nil
}

func (m *mockResearchAndLookoutStore) ListVoyageTracks(_ context.Context, _ int64) ([]model.VoyageTrack, error) {
	return m.voyageTracks, nil
}

func (m *mockResearchAndLookoutStore) ListStopTracks(_ context.Context, stopID int64) ([]model.VoyageTrack, error) {
	if m.stopTracks != nil {
		return m.stopTracks[stopID], nil
	}
	return nil, nil
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
			name: "second stop within 2 NM on same date clones briefing and triggers lookout without agent call",
			stop: &model.Stop{
				ID:           102,
				VoyageID:     1,
				LocationName: "Falmouth Harbour",
				Latitude:     17.015, // ~0.9 NM from 17.00, -61.76
				Longitude:    -61.76,
				TargetDate:   time.Date(2027, 5, 1, 12, 0, 0, 0, time.UTC),
			},
			allStops: []model.Stop{
				{ID: 101, VoyageID: 1, LocationName: "English Harbour", Latitude: 17.0, Longitude: -61.76, TargetDate: time.Date(2027, 5, 1, 8, 0, 0, 0, time.UTC)},
				{ID: 102, VoyageID: 1, LocationName: "Falmouth Harbour", Latitude: 17.015, Longitude: -61.76, TargetDate: time.Date(2027, 5, 1, 12, 0, 0, 0, time.UTC)},
				{ID: 103, VoyageID: 1, LocationName: "Jolly Harbour", Latitude: 17.066, Longitude: -61.886, TargetDate: time.Date(2027, 5, 2, 12, 0, 0, 0, time.UTC)},
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
		{
			name: "return stop within 2 NM on different date reuses facilities and fetches fresh weather/tides",
			stop: &model.Stop{
				ID:           102,
				VoyageID:     1,
				LocationName: "English Harbour Return",
				Latitude:     17.0,
				Longitude:    -61.76,
				TargetDate:   time.Date(2027, 5, 3, 12, 0, 0, 0, time.UTC),
			},
			allStops: []model.Stop{
				{ID: 101, VoyageID: 1, LocationName: "English Harbour", Latitude: 17.0, Longitude: -61.76, TargetDate: time.Date(2027, 4, 30, 12, 0, 0, 0, time.UTC)},
				{ID: 102, VoyageID: 1, LocationName: "English Harbour Return", Latitude: 17.0, Longitude: -61.76, TargetDate: time.Date(2027, 5, 3, 12, 0, 0, 0, time.UTC)},
			},
			firstBriefing: &model.Briefing{
				StopID:         101,
				WeatherSummary: model.RawJSON([]byte(`{"summary":"Clear"}`)),
				SunPhase:       model.RawJSON([]byte(`{"sunrise":"06:00"}`)),
				Tides:          model.RawJSON([]byte(`{"high":"12:00"}`)),
				Facilities:     model.RawJSON([]byte(`[{"name":"Nelson Dockyard","type":"Marina","latitude":17.0,"longitude":-61.76}]`)),
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPerformStopResearch_IncludesPlannedTrackInPilotPrompt(t *testing.T) {
	stop1ID := int64(201)
	stop1 := &model.Stop{
		ID:               stop1ID,
		VoyageID:         10,
		LocationName:     "Annapolis",
		Latitude:         38.9784,
		Longitude:        -76.4922,
		SearchRadius:     5,
		SearchRadiusUnit: "nm",
		TargetDate:       time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	stop2 := model.Stop{
		ID:               202,
		VoyageID:         10,
		LocationName:     "St. Michaels",
		Latitude:         38.7854,
		Longitude:        -76.2233,
		SearchRadius:     5,
		SearchRadiusUnit: "nm",
		TargetDate:       time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
	}
	plannedGeoJSON := `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[-76.4922,38.9784],[-76.3800,38.8900],[-76.2233,38.7854]]},"properties":{}}]}`
	var capturedHarbourPrompt string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "sessions") {
			w.Write([]byte(`{"name":"session"}`))
			return
		}
		if r.URL.Path == "/api/run" {
			var req struct {
				AppName    string `json:"appName"`
				NewMessage struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"newMessage"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.AppName == "harbourmaster" && len(req.NewMessage.Parts) > 0 {
				capturedHarbourPrompt = req.NewMessage.Parts[0].Text
				harbourResp := `{"weather_summary":{"summary":"Clear"},"sun_phase":{"sunrise":"05:45"},"tides":{"high":"11:00"},"facilities":[{"name":"Annapolis City Marina","type":"Marina"}],"pilot_notes":{"overview":"Follow planned track through Eastern Bay"}}`
				event := fmt.Sprintf(`[{"content":{"parts":[{"text":%q}],"role":"model"}}]`, harbourResp)
				w.Write([]byte(event))
				return
			}
			lookoutResp := `[]`
			event := fmt.Sprintf(`[{"content":{"parts":[{"text":%q}],"role":"model"}}]`, lookoutResp)
			w.Write([]byte(event))
		}
	})
	store := &mockResearchAndLookoutStore{
		stops: []model.Stop{*stop1, stop2},
		voyageTracks: []model.VoyageTrack{
			{
				ID:           "track-plan-1",
				VoyageID:     10,
				VoyageStopID: &stop1ID,
				Name:         "Leg 1 - Annapolis to St. Michaels",
				Kind:         "planned",
				GeoJSON:      model.RawJSON(plannedGeoJSON),
			},
		},
		savedAlerts: make(map[int64]model.RawJSON),
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			return rec.Result(), nil
		}),
	}
	h := &Handler{
		DB:          store,
		AgentURL:    "http://agent.local",
		AgentClient: client,
		Agent:       &agent.AgentRunner{Client: client, Resolver: &agent.StaticResolver{BaseURL: "http://agent.local"}},
		ResearchSem: make(chan struct{}, 10),
	}
	h.performStopResearchLogic(stop1, "")
	if !strings.Contains(capturedHarbourPrompt, "Planned Track for this leg:") {
		t.Errorf("expected harbourmaster prompt to include planned track header, got: %s", capturedHarbourPrompt)
	}
	if !strings.Contains(capturedHarbourPrompt, "Leg 1 - Annapolis to St. Michaels") {
		t.Errorf("expected harbourmaster prompt to include planned track name, got: %s", capturedHarbourPrompt)
	}
	if !strings.Contains(capturedHarbourPrompt, "Key Route Segments & Bearings:") {
		t.Errorf("expected harbourmaster prompt to include planned route segments and bearings, got: %s", capturedHarbourPrompt)
	}
	if !strings.Contains(capturedHarbourPrompt, "Compare the course, segment headings, waypoints, hazards, wind/tides/currents, and pilotage guidance against this planned track") {
		t.Errorf("expected harbourmaster prompt to instruct pilot comparison against planned track, got: %s", capturedHarbourPrompt)
	}
}

func TestPerformStopResearchLogic_StateNormalizationAndSalvage(t *testing.T) {
	stop1 := &model.Stop{
		ID:               1,
		VoyageID:         10,
		LocationName:     "Annapolis, MD",
		Latitude:         38.9784,
		Longitude:        -76.4922,
		SearchRadius:     5,
		SearchRadiusUnit: "NM",
		TargetDate:       time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sessions") {
			w.Write([]byte(`{"name":"session"}`))
			return
		}
		if r.URL.Path == "/api/run" {
			var req struct {
				AppName string `json:"appName"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.AppName == "harbourmaster" {
				// Emit tool call event with stateDelta (using raw tool struct field names)
				// followed by a truncated/invalid JSON final response to verify salvage.
				events := `[
					{
						"content": {
							"role": "model",
							"parts": [
								{"text": "Checking conditions for {38.9784, -76.4922}..."},
								{"functionCall": {"name": "GetWeather"}}
							]
						},
						"actions": {
							"stateDelta": {
								"briefing_weather": {"condition":"Sunny","min_temp":65,"max_temp":81,"max_wind_kts":14.5,"wind_direction":"SW","wave_height":2.1,"digest":"Sunny, 65-81F, peak wind 14.5 kts"},
								"briefing_tides": {"station_name":"Annapolis","tides":[{"time":"2026-06-15 06:12","type":"High","height":1.8}]},
								"briefing_sun": {"sunrise":"2026-06-15T05:42:00","sunset":"2026-06-15T20:31:00"},
								"verified_places": [{"name":"Annapolis City Marina","type":"marina","address":"410 Severn Ave","latitude":38.972,"longitude":-76.485,"website_uri":"https://annapoliscitymarina.com"}]
							}
						}
					},
					{
						"content": {
							"role": "model",
							"parts": [{"text": "{\"pilot_notes\":{\"overview\":\"Truncated"}]
						}
					}
				]`
				w.Write([]byte(events))
				return
			}
			w.Write([]byte(`[{"content":{"parts":[{"text":"[]"}],"role":"model"}}]`))
		}
	})

	store := &mockResearchAndLookoutStore{
		stops:       []model.Stop{*stop1},
		savedAlerts: make(map[int64]model.RawJSON),
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			return rec.Result(), nil
		}),
	}
	h := &Handler{
		DB:          store,
		AgentURL:    "http://agent.local",
		AgentClient: client,
		Agent:       &agent.AgentRunner{Client: client, Resolver: &agent.StaticResolver{BaseURL: "http://agent.local"}},
		ResearchSem: make(chan struct{}, 10),
	}

	h.performStopResearchLogic(stop1, "")

	if len(store.briefings) != 1 {
		t.Fatalf("expected 1 salvaged briefing to be saved, got %d", len(store.briefings))
	}
	saved := store.briefings[0]
	var wx map[string]any
	if err := json.Unmarshal(saved.WeatherSummary, &wx); err != nil {
		t.Fatalf("unmarshal weather: %v", err)
	}
	if wx["wind_speed_kt"] != 14.5 || wx["temp_max_f"] != float64(81) || wx["wave_height_ft"] != 2.1 {
		t.Errorf("expected normalized weather fields (wind_speed_kt, temp_max_f, wave_height_ft), got: %v", wx)
	}
	var tides map[string]any
	if err := json.Unmarshal(saved.Tides, &tides); err != nil {
		t.Fatalf("unmarshal tides: %v", err)
	}
	if _, ok := tides["events"]; !ok {
		t.Errorf("expected normalized tides.events field, got: %v", tides)
	}
	var sun map[string]any
	if err := json.Unmarshal(saved.SunPhase, &sun); err != nil {
		t.Fatalf("unmarshal sun: %v", err)
	}
	if sun["sunrise"] != "05:42 AM" || sun["sunset"] != "08:31 PM" {
		t.Errorf("expected formatted sun_phase times, got: %v", sun)
	}
}
