package handlers

import (
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
		name       string
		stop       *model.Stop
		distNM     float64
		course     float64
		hasCourse  bool
		stopPos    int
		totalStops int
		wantSubstr string
	}{
		{
			name: "Includes Course",
			stop: &model.Stop{
				LocationName: "Test Port",
			},
			distNM:     10.0,
			course:     45.0,
			hasCourse:  true,
			stopPos:    1,
			totalStops: 2,
			wantSubstr: "10.0 nautical miles at a course of 45°",
		},
		{
			name: "No Course Last Stop",
			stop: &model.Stop{
				LocationName: "Test Port",
			},
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
			distNM:     5.0,
			course:     90.0,
			hasCourse:  true,
			stopPos:    1,
			totalStops: 2,
			wantSubstr: "Location: Test Port (41.5000, -70.6000)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildLookoutPrompt(tt.stop, briefing, tt.distNM, tt.course, tt.hasCourse, tt.stopPos, tt.totalStops)
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
