package handlers

import (
	"context"
	"fmt"
	"testing"

	"app/internal/model"
)

func TestCalculateBearing(t *testing.T) {
	tests := []struct {
		name       string
		lat1, lon1 float64
		lat2, lon2 float64
		want       float64
	}{
		{
			name: "North",
			lat1: 0, lon1: 0,
			lat2: 1, lon2: 0,
			want: 0,
		},
		{
			name: "East",
			lat1: 0, lon1: 0,
			lat2: 0, lon2: 1,
			want: 90,
		},
		{
			name: "South",
			lat1: 1, lon1: 0,
			lat2: 0, lon2: 0,
			want: 180,
		},
		{
			name: "West",
			lat1: 0, lon1: 0,
			lat2: 0, lon2: -1,
			want: 270,
		},
		{
			name: "North-East",
			lat1: 0, lon1: 0,
			lat2: 1, lon2: 1,
			want: 44.9956,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateBearing(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if tt.name == "North-East" {
				if got < 44 || got > 46 {
					t.Errorf("calculateBearing() = %v, want approx %v", got, tt.want)
				}
			} else if got != tt.want {
				t.Errorf("calculateBearing() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildLookoutPrompt(t *testing.T) {
	briefing := &model.Briefing{
		WeatherSummary: []byte(`{"hourly": []}`),
	}

	tests := []struct {
		name           string
		stop           *model.Stop
		next           *model.Stop
		distNM         float64
		course         float64
		hasCourse      bool
		stopPos        int
		totalStops     int
		routeWaypoints []string
		wantSubstr     string
	}{
		{
			name: "Includes Course and Next Stop",
			stop: &model.Stop{
				LocationName: "Test Port",
			},
			next: &model.Stop{
				LocationName: "Destination Port",
				Latitude:     41.6000,
				Longitude:    -70.5000,
			},
			distNM:     10.0,
			course:     45.0,
			hasCourse:  true,
			stopPos:    1,
			totalStops: 2,
			wantSubstr: "10.0 nautical miles at a course of 45° to Destination Port",
		},
		{
			name: "No Course Last Stop",
			stop: &model.Stop{
				LocationName: "Test Port",
			},
			next:       nil,
			distNM:     0,
			course:     0,
			hasCourse:  false,
			stopPos:    2,
			totalStops: 2,
			wantSubstr: "none — this is the last stop",
		},
		{
			name: "Includes Coordinates",
			stop: &model.Stop{
				LocationName: "Test Port",
				Latitude:     41.5000,
				Longitude:    -70.6000,
			},
			next: &model.Stop{
				LocationName: "Next Bay",
			},
			distNM:     5.0,
			course:     90.0,
			hasCourse:  true,
			stopPos:    1,
			totalStops: 2,
			wantSubstr: "Location: Test Port (41.5000, -70.6000)",
		},
		{
			name: "Includes Planned Route",
			stop: &model.Stop{
				LocationName: "Cowes",
				Latitude:     50.7620,
				Longitude:    -1.2980,
			},
			next: &model.Stop{
				LocationName: "Yarmouth",
				Latitude:     50.7050,
				Longitude:    -1.5000,
			},
			distNM:         10.5,
			course:         245.0,
			hasCourse:      true,
			stopPos:        1,
			totalStops:     2,
			routeWaypoints: []string{"Intended Planned Route: \"Solent Western Passage\" (Total Distance: 10.5 NM)"},
			wantSubstr:     "Solent Western Passage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildLookoutPrompt(tt.stop, tt.next, briefing, tt.distNM, tt.course, tt.hasCourse, tt.stopPos, tt.totalStops, tt.routeWaypoints...)
			if !contains(prompt, tt.wantSubstr) {
				t.Errorf("Prompt did not contain expected substring %q. Got:\n%s", tt.wantSubstr, prompt)
			}
		})
	}
}

func contains(s, substr string) bool {
	return (len(s) >= len(substr)) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

func TestNormalizeLookoutAlertIcon(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"bridge", "height"},
		{"BRIDGE", "height"},
		{"vertical_clearance", "height"},
		{"clearance", "height"},
		{"wind", "air"},
		{"gale", "air"},
		{"tide", "waves"},
		{"rip", "waves"},
		{"current", "water"},
		{"sun", "light_mode"},
		{"sunset", "wb_twilight"},
		{"unknown_custom_icon", "unknown_custom_icon"},
		{"", "warning"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeLookoutAlertIcon(tt.input)
			if got != tt.want {
				t.Errorf("normalizeLookoutAlertIcon(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBearingToCardinal(t *testing.T) {
	tests := []struct {
		deg  float64
		want string
	}{
		{0, "N"},
		{10, "N"},
		{22.5, "NNE"},
		{45, "NE"},
		{90, "E"},
		{135, "SE"},
		{180, "S"},
		{225, "SW"},
		{270, "W"},
		{315, "NW"},
		{350, "N"},
		{360, "N"},
		{-10, "N"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%.1f", tt.deg), func(t *testing.T) {
			got := bearingToCardinal(tt.deg)
			if got != tt.want {
				t.Errorf("bearingToCardinal(%.1f) = %s, want %s", tt.deg, got, tt.want)
			}
		})
	}
}

func TestExtractCoordinates(t *testing.T) {
	tests := []struct {
		name    string
		raw     model.RawJSON
		wantLen int
	}{
		{
			name:    "Empty JSON",
			raw:     model.RawJSON(""),
			wantLen: 0,
		},
		{
			name:    "Standard Feature LineString",
			raw:     model.RawJSON(`{"type":"Feature","geometry":{"type":"LineString","coordinates":[[-70.5,41.5],[-70.4,41.6]]}}`),
			wantLen: 2,
		},
		{
			name:    "Direct LineString Geometry",
			raw:     model.RawJSON(`{"type":"LineString","coordinates":[[-70.5,41.5],[-70.4,41.6],[-70.3,41.7]]}`),
			wantLen: 3,
		},
		{
			name:    "FeatureCollection",
			raw:     model.RawJSON(`{"type":"FeatureCollection","features":[{"geometry":{"type":"LineString","coordinates":[[-70.5,41.5],[-70.4,41.6]]}}]}`),
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coords := extractCoordinates(tt.raw)
			if len(coords) != tt.wantLen {
				t.Errorf("extractCoordinates() returned %d coords, want %d", len(coords), tt.wantLen)
			}
		})
	}
}

func TestExtractWaypointsSummary(t *testing.T) {
	raw := model.RawJSON(`{"type":"Feature","geometry":{"coordinates":[[-70.5,41.5],[-70.4,41.6]]}}`)
	summary := extractWaypointsSummary(raw)
	if !contains(summary, "(41.5000, -70.5000) -> (41.6000, -70.4000)") {
		t.Errorf("extractWaypointsSummary() = %q, want coordinates", summary)
	}

	emptySummary := extractWaypointsSummary(nil)
	if emptySummary != "" {
		t.Errorf("extractWaypointsSummary(nil) = %q, want empty", emptySummary)
	}
}

func TestFormatPlannedRouteForLookout(t *testing.T) {
	dist := 12.5
	tr := &model.VoyageTrack{
		Name:              "Solent Route",
		DistanceNM:        &dist,
		SimplifiedGeoJSON: model.RawJSON(`{"type":"Feature","geometry":{"coordinates":[[-1.298,50.762],[-1.390,50.730],[-1.480,50.710],[-1.500,50.705]]}}`),
	}

	info, calcDist := formatPlannedRouteForLookout(tr, 10.0)
	if calcDist != 12.5 {
		t.Errorf("formatPlannedRouteForLookout dist = %v, want 12.5", calcDist)
	}
	if !contains(info, "Solent Route") {
		t.Errorf("formatPlannedRouteForLookout info missing route name: %s", info)
	}
	if !contains(info, "Key Route Segments & Bearings") {
		t.Errorf("formatPlannedRouteForLookout info missing segments: %s", info)
	}
	if !contains(info, "Full Planned Waypoints Sequence") {
		t.Errorf("formatPlannedRouteForLookout info missing waypoints sequence: %s", info)
	}

	nilInfo, nilDist := formatPlannedRouteForLookout(nil, 5.0)
	if nilInfo != "" || nilDist != 5.0 {
		t.Errorf("formatPlannedRouteForLookout(nil) = (%q, %v), want ('', 5.0)", nilInfo, nilDist)
	}
}

type testPlannedTrackStore struct {
	DBStore
	voyageTracks []model.VoyageTrack
	stopTracks   map[int64][]model.VoyageTrack
}

func (s *testPlannedTrackStore) ListVoyageTracks(_ context.Context, _ int64) ([]model.VoyageTrack, error) {
	return s.voyageTracks, nil
}

func (s *testPlannedTrackStore) ListStopTracks(_ context.Context, stopID int64) ([]model.VoyageTrack, error) {
	if s.stopTracks != nil {
		return s.stopTracks[stopID], nil
	}
	return nil, nil
}

func TestFindPlannedTrackForLeg(t *testing.T) {
	stopID1 := int64(10)
	stopID2 := int64(20)
	dist1 := 15.0

	tests := []struct {
		name         string
		voyageTracks []model.VoyageTrack
		stopTracks   map[int64][]model.VoyageTrack
		stop         *model.Stop
		next         *model.Stop
		stopPos      int
		wantName     string
	}{
		{
			name: "Direct match on departure stop ID",
			voyageTracks: []model.VoyageTrack{
				{
					Name:         "Leg 1 Track",
					Kind:         string(model.TrackKindPlanned),
					VoyageStopID: &stopID1,
					DistanceNM:   &dist1,
				},
			},
			stop:     &model.Stop{ID: 10, VoyageID: 1},
			next:     &model.Stop{ID: 20, VoyageID: 1},
			stopPos:  1,
			wantName: "Leg 1 Track",
		},
		{
			name: "Match on destination stop ID",
			voyageTracks: []model.VoyageTrack{
				{
					Name:         "Leg 2 Track",
					Kind:         string(model.TrackKindPlanned),
					VoyageStopID: &stopID2,
					DistanceNM:   &dist1,
				},
			},
			stop:     &model.Stop{ID: 10, VoyageID: 1},
			next:     &model.Stop{ID: 20, VoyageID: 1},
			stopPos:  1,
			wantName: "Leg 2 Track",
		},
		{
			name: "Match on Leg number in track name",
			voyageTracks: []model.VoyageTrack{
				{
					Name:       "Passage Leg 2",
					Kind:       string(model.TrackKindPlanned),
					DistanceNM: &dist1,
				},
			},
			stop:     &model.Stop{ID: 15, VoyageID: 1},
			next:     &model.Stop{ID: 25, VoyageID: 1},
			stopPos:  2,
			wantName: "Passage Leg 2",
		},
		{
			name: "Single planned track fallback",
			voyageTracks: []model.VoyageTrack{
				{
					Name:       "Sole Passage",
					Kind:       string(model.TrackKindPlanned),
					DistanceNM: &dist1,
				},
			},
			stop:     &model.Stop{ID: 10, VoyageID: 1},
			next:     &model.Stop{ID: 20, VoyageID: 1},
			stopPos:  1,
			wantName: "Sole Passage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &testPlannedTrackStore{
				voyageTracks: tt.voyageTracks,
				stopTracks:   tt.stopTracks,
			}
			h := &Handler{DB: mockStore}
			matched := h.findPlannedTrackForLeg(context.Background(), tt.stop, tt.next, tt.stopPos)
			if matched == nil {
				t.Fatalf("findPlannedTrackForLeg returned nil, want track %q", tt.wantName)
			}
			if matched.Name != tt.wantName {
				t.Errorf("findPlannedTrackForLeg() = %q, want %q", matched.Name, tt.wantName)
			}
		})
	}
}
