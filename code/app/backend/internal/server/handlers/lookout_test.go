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
	stop := &model.Stop{
		LocationName: "Test Port",
	}
	briefing := &model.Briefing{
		WeatherSummary: []byte(`{"hourly": []}`),
	}

	t.Run("Includes Course", func(t *testing.T) {
		prompt := buildLookoutPrompt(stop, briefing, 10.0, 45.0, true, 1, 2)
		expected := "10.0 nautical miles at a course of 45°"
		if !contains(prompt, expected) {
			t.Errorf("Prompt did not contain expected course info. Got: %s", prompt)
		}
	})

	t.Run("No Course", func(t *testing.T) {
		prompt := buildLookoutPrompt(stop, briefing, 0, 0, false, 2, 2)
		expected := "none — this is the last stop"
		if !contains(prompt, expected) {
			t.Errorf("Prompt did not contain expected no-departure info. Got: %s", prompt)
		}
	})
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
