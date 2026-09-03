package tool

import (
	"math"
	"testing"
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
			got := CalculateBearing(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if tt.name == "North-East" {
				if got < 44 || got > 46 {
					t.Errorf("CalculateBearing() = %v, want approx %v", got, tt.want)
				}
			} else if got != tt.want {
				t.Errorf("CalculateBearing() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateDistanceNM(t *testing.T) {
	tests := []struct {
		name       string
		lat1, lon1 float64
		lat2, lon2 float64
		wantMin    float64
		wantMax    float64
	}{
		{
			name: "Same point",
			lat1: 41.5, lon1: -70.5,
			lat2: 41.5, lon2: -70.5,
			wantMin: 0, wantMax: 0.001,
		},
		{
			name: "One degree latitude north (approx 60 NM)",
			lat1: 0, lon1: 0,
			lat2: 1, lon2: 0,
			wantMin: 59.5, wantMax: 60.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateDistanceNM(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CalculateDistanceNM() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCalculateReciprocalHeading(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  float64
	}{
		{name: "North to South", input: 0, want: 180},
		{name: "East to West", input: 90, want: 270},
		{name: "South to North", input: 180, want: 0},
		{name: "West to East", input: 270, want: 90},
		{name: "45 to 225", input: 45, want: 225},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateReciprocalHeading(tt.input)
			if got != tt.want {
				t.Errorf("CalculateReciprocalHeading(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRelativeWindAngle(t *testing.T) {
	tests := []struct {
		name      string
		courseDeg float64
		windDir   float64
		want      float64
	}{
		{name: "Direct headwind", courseDeg: 90, windDir: 90, want: 0},
		{name: "Direct tailwind", courseDeg: 90, windDir: 270, want: 180},
		{name: "Beam reach starboard", courseDeg: 90, windDir: 180, want: 90},
		{name: "Wrap around 0 across North 1", courseDeg: 10, windDir: 350, want: 20},
		{name: "Wrap around 0 across North 2", courseDeg: 350, windDir: 10, want: 20},
		{name: "Beating close hauled 30 deg off", courseDeg: 45, windDir: 75, want: 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelativeWindAngle(tt.courseDeg, tt.windDir)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("RelativeWindAngle(%v, %v) = %v, want %v", tt.courseDeg, tt.windDir, got, tt.want)
			}
		})
	}
}

func TestCalculateHeadingResult(t *testing.T) {
	windDirHeadwind := 85.0
	windDirTailwind := 265.0
	windSpeedGale := 22.0
	windSpeedModerate := 12.0
	windSpeedLight := 3.0

	tests := []struct {
		name            string
		req             HeadingRequest
		wantAdverse     bool
		wantSeverity    string
		wantRelation    string
		wantHeadingNear float64
	}{
		{
			name: "Without wind data",
			req: HeadingRequest{
				FromLatitude:  41.42,
				FromLongitude: -70.92,
				ToLatitude:    41.45,
				ToLongitude:   -70.58,
				FromLocation:  "Cuttyhunk",
				ToLocation:    "Vineyard Haven",
			},
			wantAdverse:     false,
			wantSeverity:    "none",
			wantRelation:    "",
			wantHeadingNear: 83.0,
		},
		{
			name: "Heavy adverse wind (headwind > 15 kts)",
			req: HeadingRequest{
				FromLatitude:     41.42,
				FromLongitude:    -70.92,
				ToLatitude:       41.45,
				ToLongitude:      -70.58,
				FromLocation:     "Cuttyhunk",
				ToLocation:       "Vineyard Haven",
				WindDirectionDeg: &windDirHeadwind,
				WindSpeedKts:     &windSpeedGale,
			},
			wantAdverse:     true,
			wantSeverity:    "warning",
			wantRelation:    "headwind",
			wantHeadingNear: 83.0,
		},
		{
			name: "Moderate adverse wind (headwind 5-15 kts)",
			req: HeadingRequest{
				FromLatitude:     41.42,
				FromLongitude:    -70.92,
				ToLatitude:       41.45,
				ToLongitude:      -70.58,
				FromLocation:     "Cuttyhunk",
				ToLocation:       "Vineyard Haven",
				WindDirectionDeg: &windDirHeadwind,
				WindSpeedKts:     &windSpeedModerate,
			},
			wantAdverse:     true,
			wantSeverity:    "info",
			wantRelation:    "headwind",
			wantHeadingNear: 83.0,
		},
		{
			name: "Light headwind (< 5 kts, not adverse)",
			req: HeadingRequest{
				FromLatitude:     41.42,
				FromLongitude:    -70.92,
				ToLatitude:       41.45,
				ToLongitude:      -70.58,
				FromLocation:     "Cuttyhunk",
				ToLocation:       "Vineyard Haven",
				WindDirectionDeg: &windDirHeadwind,
				WindSpeedKts:     &windSpeedLight,
			},
			wantAdverse:     false,
			wantSeverity:    "none",
			wantRelation:    "headwind",
			wantHeadingNear: 83.0,
		},
		{
			name: "Tailwind high speed (not adverse)",
			req: HeadingRequest{
				FromLatitude:     41.42,
				FromLongitude:    -70.92,
				ToLatitude:       41.45,
				ToLongitude:      -70.58,
				FromLocation:     "Cuttyhunk",
				ToLocation:       "Vineyard Haven",
				WindDirectionDeg: &windDirTailwind,
				WindSpeedKts:     &windSpeedGale,
			},
			wantAdverse:     false,
			wantSeverity:    "none",
			wantRelation:    "tailwind",
			wantHeadingNear: 83.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := CalculateHeadingResult(tt.req)
			if math.Abs(res.HeadingDegrees-tt.wantHeadingNear) > 5.0 {
				t.Errorf("HeadingDegrees = %v, want near %v", res.HeadingDegrees, tt.wantHeadingNear)
			}
			if res.IsAdverseWind != tt.wantAdverse {
				t.Errorf("IsAdverseWind = %v, want %v", res.IsAdverseWind, tt.wantAdverse)
			}
			if res.AdverseWindSeverity != tt.wantSeverity {
				t.Errorf("AdverseWindSeverity = %q, want %q", res.AdverseWindSeverity, tt.wantSeverity)
			}
			if res.WindRelation != tt.wantRelation {
				t.Errorf("WindRelation = %q, want %q", res.WindRelation, tt.wantRelation)
			}
			if res.Summary == "" {
				t.Error("Summary should not be empty")
			}
		})
	}
}
