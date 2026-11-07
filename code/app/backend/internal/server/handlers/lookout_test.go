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
		day            *model.VoyageDay
		distNM         float64
		course         float64
		hasCourse      bool
		dayPos         int
		totalDays      int
		routeWaypoints []string
		wantSubstr     string
	}{
		{
			name: "Includes Course and Destination",
			day: &model.VoyageDay{
				StartLocationName: "Test Port",
				EndLocationName:   "Destination Port",
				EndLatitude:       41.6000,
				EndLongitude:      -70.5000,
			},
			distNM:     10.0,
			course:     45.0,
			hasCourse:  true,
			dayPos:     1,
			totalDays:  2,
			wantSubstr: "10.0 nautical miles at a course of 45° to Destination Port",
		},
		{
			name: "No Course Stationary Day",
			day: &model.VoyageDay{
				StartLocationName: "Test Port",
				EndLocationName:   "Test Port",
			},
			distNM:     0,
			course:     0,
			hasCourse:  false,
			dayPos:     2,
			totalDays:  2,
			wantSubstr: "none — stationary day",
		},
		{
			name: "Includes Coordinates",
			day: &model.VoyageDay{
				StartLocationName: "Test Port",
				StartLatitude:     41.5000,
				StartLongitude:    -70.6000,
				EndLocationName:   "Next Bay",
			},
			distNM:     5.0,
			course:     90.0,
			hasCourse:  true,
			dayPos:     1,
			totalDays:  2,
			wantSubstr: "Departure Location: Test Port (41.5000, -70.6000)",
		},
		{
			name: "Includes Planned Route",
			day: &model.VoyageDay{
				StartLocationName: "Cowes",
				StartLatitude:     50.7620,
				StartLongitude:    -1.2980,
				EndLocationName:   "Yarmouth",
				EndLatitude:       50.7050,
				EndLongitude:      -1.5000,
			},
			distNM:         10.5,
			course:         245.0,
			hasCourse:      true,
			dayPos:         1,
			totalDays:      2,
			routeWaypoints: []string{"Intended Planned Route: \"Solent Western Passage\" (Total Distance: 10.5 NM)"},
			wantSubstr:     "Solent Western Passage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildLookoutPrompt(tt.day, briefing, tt.distNM, tt.course, tt.hasCourse, tt.dayPos, tt.totalDays, tt.routeWaypoints...)
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
	dayTracks    map[int64][]model.VoyageTrack
}

func (s *testPlannedTrackStore) ListVoyageTracks(_ context.Context, _ int64) ([]model.VoyageTrack, error) {
	return s.voyageTracks, nil
}

func (s *testPlannedTrackStore) ListDayTracks(_ context.Context, dayID int64) ([]model.VoyageTrack, error) {
	if s.dayTracks != nil {
		return s.dayTracks[dayID], nil
	}
	return nil, nil
}

func TestFindPlannedTrackForDay(t *testing.T) {
	dayID1 := int64(10)
	dist1 := 15.0

	tests := []struct {
		name         string
		voyageTracks []model.VoyageTrack
		dayTracks    map[int64][]model.VoyageTrack
		day          *model.VoyageDay
		dayPos       int
		wantName     string
	}{
		{
			name: "Direct match on day ID",
			voyageTracks: []model.VoyageTrack{
				{
					Name:        "Leg 1 Track",
					Kind:        string(model.TrackKindPlanned),
					VoyageDayID: &dayID1,
					DistanceNM:  &dist1,
				},
			},
			day:      &model.VoyageDay{ID: 10, VoyageID: 1},
			dayPos:   1,
			wantName: "Leg 1 Track",
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
			day:      &model.VoyageDay{ID: 15, VoyageID: 1},
			dayPos:   2,
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
			day:      &model.VoyageDay{ID: 10, VoyageID: 1},
			dayPos:   1,
			wantName: "Sole Passage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &testPlannedTrackStore{
				voyageTracks: tt.voyageTracks,
				dayTracks:    tt.dayTracks,
			}
			h := &Handler{DB: mockStore}
			matched := h.findPlannedTrackForDay(context.Background(), tt.day, tt.dayPos)
			if matched == nil {
				t.Fatalf("findPlannedTrackForDay returned nil, want track %q", tt.wantName)
			}
			if matched.Name != tt.wantName {
				t.Errorf("findPlannedTrackForDay() = %q, want %q", matched.Name, tt.wantName)
			}
		})
	}
}

func TestFindPlannedTracksForDay_MultipleCandidates(t *testing.T) {
	dayID1 := int64(10)
	distA := 14.2
	distB := 18.6
	distMaster := 42.0
	fileA := "plan_a.gpx"
	fileB := "plan_b.gpx"

	mockStore := &testPlannedTrackStore{
		voyageTracks: []model.VoyageTrack{
			{
				ID:         "master-a",
				Name:       "Plan A Inside",
				FileName:   &fileA,
				Kind:       string(model.TrackKindPlanned),
				DistanceNM: &distMaster,
			},
			{
				ID:                "leg1-a",
				Name:              "Plan A Inside - Leg 1: Cowes to Yarmouth",
				FileName:          &fileA,
				Kind:              string(model.TrackKindPlanned),
				VoyageDayID:       &dayID1,
				DistanceNM:        &distA,
				SimplifiedGeoJSON: model.RawJSON(`{"type":"Feature","geometry":{"coordinates":[[-1.298,50.762],[-1.390,50.730],[-1.500,50.705]]}}`),
			},
			{
				ID:                "leg1-b",
				Name:              "Plan B Offshore - Leg 1: Cowes to Yarmouth",
				FileName:          &fileB,
				Kind:              string(model.TrackKindPlanned),
				VoyageDayID:       &dayID1,
				DistanceNM:        &distB,
				SimplifiedGeoJSON: model.RawJSON(`{"type":"Feature","geometry":{"coordinates":[[-1.298,50.762],[-1.420,50.680],[-1.500,50.705]]}}`),
			},
		},
	}

	h := &Handler{DB: mockStore}
	day := &model.VoyageDay{ID: 10, VoyageID: 1}
	matched := h.findPlannedTracksForDay(context.Background(), day, 1)
	if len(matched) != 2 {
		t.Fatalf("findPlannedTracksForDay returned %d tracks, want 2 (excluding master track)", len(matched))
	}
	if matched[0].ID != "leg1-a" || matched[1].ID != "leg1-b" {
		t.Errorf("findPlannedTracksForDay IDs = [%s, %s], want [leg1-a, leg1-b]", matched[0].ID, matched[1].ID)
	}

	info, primaryDist := formatPlannedRoutesForLookout(matched, 10.0)
	if primaryDist != distA {
		t.Errorf("formatPlannedRoutesForLookout primaryDist = %v, want %v", primaryDist, distA)
	}
	if !contains(info, "Candidate Planned Route 1 of 2") || !contains(info, "Candidate Planned Route 2 of 2") {
		t.Errorf("formatPlannedRoutesForLookout missing candidate route headers:\n%s", info)
	}
	if !contains(info, "Travel Time Table for") || !contains(info, "Multi-Plan Comparison Instructions") {
		t.Errorf("formatPlannedRoutesForLookout missing multi-route comparison instructions:\n%s", info)
	}
}

func TestResolveGPXTrackNameAndMultiPlanComparison(t *testing.T) {
	if got := resolveGPXTrackName("Track 1", "North_Channel_Route.gpx", ""); got != "North_Channel_Route" {
		t.Errorf("resolveGPXTrackName generic = %q, want %q", got, "North_Channel_Route")
	}
	if got := resolveGPXTrackName("Solent Inside Route", "North_Channel_Route.gpx", ""); got != "Solent Inside Route" {
		t.Errorf("resolveGPXTrackName explicit = %q, want %q", got, "Solent Inside Route")
	}

	dayID1 := int64(10)
	distA := 12.4
	distB := 17.8
	candidates := []*model.VoyageTrack{
		{
			ID:                "plan-a",
			VoyageID:          1,
			VoyageDayID:       &dayID1,
			Name:              "Inside Passage Plan A",
			Kind:              string(model.TrackKindPlanned),
			DistanceNM:        &distA,
			SimplifiedGeoJSON: model.RawJSON(`{"type":"Feature","geometry":{"coordinates":[[-1.298,50.762],[-1.390,50.730],[-1.500,50.705]]}}`),
		},
		{
			ID:                "plan-b",
			VoyageID:          1,
			VoyageDayID:       &dayID1,
			Name:              "Outside Channel Plan B",
			Kind:              string(model.TrackKindPlanned),
			DistanceNM:        &distB,
			SimplifiedGeoJSON: model.RawJSON(`{"type":"Feature","geometry":{"coordinates":[[-1.298,50.762],[-1.420,50.680],[-1.500,50.705]]}}`),
		},
	}
	days := []model.VoyageDay{
		{
			ID:                10,
			VoyageID:          1,
			StartLocationName: "Cowes",
			EndLocationName:   "Yarmouth",
		},
	}

	h := &Handler{}
	comp := h.generateMultiPlanComparison(context.Background(), candidates, &days[0], days)
	if comp == nil {
		t.Fatalf("generateMultiPlanComparison returned nil")
	}
	if comp.Mode != "comparison" {
		t.Errorf("comp.Mode = %q, want 'comparison'", comp.Mode)
	}
	if len(comp.CandidateTracks) != 2 {
		t.Errorf("len(comp.CandidateTracks) = %d, want 2", len(comp.CandidateTracks))
	}
	if comp.RecommendedTrackName == "" || comp.RecommendedTrackID == nil {
		t.Errorf("expected non-empty RecommendedTrackName and RecommendedTrackID, got %q / %v", comp.RecommendedTrackName, comp.RecommendedTrackID)
	}
	if comp.Summary == "" || comp.WeatherImpact == "" || comp.TidalAnalysis == "" {
		t.Errorf("expected populated Summary, WeatherImpact, and TidalAnalysis, got: %+v", comp)
	}
	if !contains(comp.TidalAnalysis, "velocity made good (VMG)") {
		t.Errorf("expected first occurrence of VMG to be expanded to 'velocity made good (VMG)', got: %q", comp.TidalAnalysis)
	}
	if len(comp.CandidateTracks[0].AlongRouteProfile) == 0 {
		t.Errorf("expected populated AlongRouteProfile on candidate track, got empty")
	}

	ws := &model.WeatherSummary{
		WindSpeedKt:   18,
		WindDirection: "SW",
		WaveHeightFt:  3.5,
		HourlyWind:    []float64{12, 12, 14, 14, 15, 16, 17, 18, 19, 20, 21, 22, 20},
		HourlyWindDir: []string{"SW", "SW", "SW", "SW", "SW", "SW", "SW", "SW", "WSW", "WSW", "W", "W", "W"},
	}
	rawTides := []byte(`{"station_name":"Cowes","events":[{"time":"2026-06-15 06:30","type":"Low","height_ft":1.2},{"time":"2026-06-15 12:45","type":"High","height_ft":6.4}]}`)
	ev := evaluateCandidateRoute(candidates[0], ws, rawTides)
	if ev.candidate.TidalSummary == "" || !contains(ev.candidate.TidalSummary, "Flooding") {
		t.Errorf("expected TidalSummary with Flooding stage, got %q", ev.candidate.TidalSummary)
	}
	if len(ev.candidate.AlongRouteProfile) == 0 || !contains(ev.candidate.AlongRouteProfile[0], "Flooding") {
		t.Errorf("expected AlongRouteProfile with time-stepped wind and tide stage, got %v", ev.candidate.AlongRouteProfile)
	}
}
