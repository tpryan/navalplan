package gpx

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestParseGPX(t *testing.T) {
	routeXML := `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="NavalPlan" xmlns="http://www.topografix.com/GPX/1/1">
  <metadata>
    <name>Solent Cruise</name>
  </metadata>
  <rte>
    <name>Cowes to Yarmouth</name>
    <rtept lat="50.76" lon="-1.30"><name>Cowes</name></rtept>
    <rtept lat="50.73" lon="-1.40"><name>Newtown Creek</name></rtept>
    <rtept lat="50.71" lon="-1.50"><name>Yarmouth</name></rtept>
  </rte>
</gpx>`

	trackXML := `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="Chartplotter" xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>Recorded Leg 1</name>
    <trkseg>
      <trkpt lat="50.7600" lon="-1.3000">
        <time>2026-07-01T10:00:00Z</time>
        <ele>0.5</ele>
        <speed>3.086</speed> <!-- ~6.0 knots -->
      </trkpt>
      <trkpt lat="50.7300" lon="-1.4000">
        <time>2026-07-01T11:00:00Z</time>
        <ele>0.4</ele>
        <speed>3.601</speed> <!-- ~7.0 knots -->
      </trkpt>
      <trkpt lat="50.7100" lon="-1.5000">
        <time>2026-07-01T12:00:00Z</time>
        <ele>0.2</ele>
        <speed>2.572</speed> <!-- ~5.0 knots -->
      </trkpt>
    </trkseg>
  </trk>
</gpx>`

	waypointXML := `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="GPS">
  <metadata><name>Points of Interest</name></metadata>
  <wpt lat="48.5" lon="-123.1"><name>WP1</name></wpt>
  <wpt lat="48.6" lon="-123.2"><name>WP2</name></wpt>
</gpx>`

	tests := []struct {
		name          string
		input         string
		preferredKind string
		wantErr       bool
		wantTracks    int
		wantKind      string
		wantMinDistNM float64
	}{
		{
			name:          "parse planned route",
			input:         routeXML,
			preferredKind: "",
			wantErr:       false,
			wantTracks:    1,
			wantKind:      "planned",
			wantMinDistNM: 7.0,
		},
		{
			name:          "parse recorded track with speed and timestamps",
			input:         trackXML,
			preferredKind: "",
			wantErr:       false,
			wantTracks:    1,
			wantKind:      "recorded",
			wantMinDistNM: 7.0,
		},
		{
			name:          "parse waypoint fallback as planned",
			input:         waypointXML,
			preferredKind: "",
			wantErr:       false,
			wantTracks:    1,
			wantKind:      "planned",
			wantMinDistNM: 5.0,
		},
		{
			name:          "empty gpx errors",
			input:         `<gpx version="1.1"></gpx>`,
			preferredKind: "",
			wantErr:       true,
		},
		{
			name:          "invalid xml errors",
			input:         `not xml at all`,
			preferredKind: "",
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracks, err := ParseGPX([]byte(tc.input), tc.preferredKind)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseGPX() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(tracks) != tc.wantTracks {
				t.Fatalf("ParseGPX() returned %d tracks, want %d", len(tracks), tc.wantTracks)
			}
			tr := tracks[0]
			if tr.Kind != tc.wantKind {
				t.Errorf("Track kind = %v, want %v", tr.Kind, tc.wantKind)
			}
			if tr.DistanceNM < tc.wantMinDistNM {
				t.Errorf("Track distance = %v, want at least %v", tr.DistanceNM, tc.wantMinDistNM)
			}
			if len(tr.GeoJSON) == 0 {
				t.Errorf("Expected non-empty GeoJSON")
			}
			var feat GeoJSONFeature
			if err := json.Unmarshal(tr.GeoJSON, &feat); err != nil {
				t.Errorf("Failed to unmarshal GeoJSON: %v", err)
			}
			if feat.Geometry.Type != "LineString" {
				t.Errorf("GeoJSON geometry type = %v, want LineString", feat.Geometry.Type)
			}
		})
	}
}

func TestHaversineAndBearing(t *testing.T) {
	tests := []struct {
		name        string
		lat1, lon1  float64
		lat2, lon2  float64
		wantDistMin float64
		wantDistMax float64
		wantCourse  float64
	}{
		{
			name:        "1 degree latitude due north (~60 NM)",
			lat1:        50.0,
			lon1:        -1.0,
			lat2:        51.0,
			lon2:        -1.0,
			wantDistMin: 59.8,
			wantDistMax: 60.2,
			wantCourse:  0.0,
		},
		{
			name:        "Due east along equator (~60 NM)",
			lat1:        0.0,
			lon1:        0.0,
			lat2:        0.0,
			lon2:        1.0,
			wantDistMin: 59.8,
			wantDistMax: 60.2,
			wantCourse:  90.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dist := CalculateHaversineNM(tc.lat1, tc.lon1, tc.lat2, tc.lon2)
			if dist < tc.wantDistMin || dist > tc.wantDistMax {
				t.Errorf("Haversine distance = %v, want between %v and %v", dist, tc.wantDistMin, tc.wantDistMax)
			}
			bearing := CalculateBearing(tc.lat1, tc.lon1, tc.lat2, tc.lon2)
			if math.Abs(bearing-tc.wantCourse) > 1.0 {
				t.Errorf("Bearing = %v, want approx %v", bearing, tc.wantCourse)
			}
		})
	}
}

func TestFormatPostgresInterval(t *testing.T) {
	tests := []struct {
		name     string
		dur      time.Duration
		expected string
	}{
		{name: "zero duration", dur: 0, expected: "00:00:00"},
		{name: "one hour thirty minutes", dur: 90 * time.Minute, expected: "01:30:00"},
		{name: "25 hours 12 minutes 45 seconds", dur: 25*time.Hour + 12*time.Minute + 45*time.Second, expected: "25:12:45"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatPostgresInterval(tc.dur)
			if got != tc.expected {
				t.Errorf("FormatPostgresInterval() = %v, want %v", got, tc.expected)
			}
		})
	}
}
