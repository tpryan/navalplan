package gpx

import (
	"testing"
)

func TestSimplifyRDP(t *testing.T) {
	tests := []struct {
		name       string
		points     []Point
		epsilonNM  float64
		wantCount  int
		checkFirst bool
		checkLast  bool
	}{
		{
			name:       "empty points",
			points:     []Point{},
			epsilonNM:  0.01,
			wantCount:  0,
			checkFirst: false,
		},
		{
			name: "two points",
			points: []Point{
				{Lat: 48.0, Lng: -123.0},
				{Lat: 48.1, Lng: -123.1},
			},
			epsilonNM:  0.01,
			wantCount:  2,
			checkFirst: true,
			checkLast:  true,
		},
		{
			name: "collinear points reduced to endpoints",
			points: []Point{
				{Lat: 48.00, Lng: -123.00},
				{Lat: 48.01, Lng: -123.01},
				{Lat: 48.02, Lng: -123.02},
				{Lat: 48.03, Lng: -123.03},
				{Lat: 48.04, Lng: -123.04},
			},
			epsilonNM:  0.05,
			wantCount:  2,
			checkFirst: true,
			checkLast:  true,
		},
		{
			name: "sharp bend preserved",
			points: []Point{
				{Lat: 48.00, Lng: -123.00},
				{Lat: 48.05, Lng: -123.00}, // 3 NM deviation
				{Lat: 48.00, Lng: -123.10},
			},
			epsilonNM:  0.05,
			wantCount:  3,
			checkFirst: true,
			checkLast:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SimplifyRDP(tc.points, tc.epsilonNM)
			if len(got) != tc.wantCount {
				t.Fatalf("SimplifyRDP() got %d points, want %d", len(got), tc.wantCount)
			}
			if tc.checkFirst && len(got) > 0 {
				if got[0].Lat != tc.points[0].Lat || got[0].Lng != tc.points[0].Lng {
					t.Errorf("First point mismatch: got (%v, %v), want (%v, %v)", got[0].Lat, got[0].Lng, tc.points[0].Lat, tc.points[0].Lng)
				}
			}
			if tc.checkLast && len(got) > 1 {
				lastIdx := len(tc.points) - 1
				gotLastIdx := len(got) - 1
				if got[gotLastIdx].Lat != tc.points[lastIdx].Lat || got[gotLastIdx].Lng != tc.points[lastIdx].Lng {
					t.Errorf("Last point mismatch: got (%v, %v), want (%v, %v)", got[gotLastIdx].Lat, got[gotLastIdx].Lng, tc.points[lastIdx].Lat, tc.points[lastIdx].Lng)
				}
			}
		})
	}
}
