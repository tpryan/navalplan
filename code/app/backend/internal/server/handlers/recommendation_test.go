package handlers

import (
	"math"
	"strings"
	"testing"
)

func TestHaversine(t *testing.T) {
	tests := []struct {
		name     string
		lat1     float64
		lon1     float64
		lat2     float64
		lon2     float64
		unit     string
		expected float64
		tol      float64
	}{
		{
			name:     "Same point",
			lat1:     18.45,
			lon1:     -64.62,
			lat2:     18.45,
			lon2:     -64.62,
			unit:     "nm",
			expected: 0.0,
			tol:      0.001,
		},
		{
			name:     "1 degree Latitude (approx 60nm)",
			lat1:     18.0,
			lon1:     -64.0,
			lat2:     19.0,
			lon2:     -64.0,
			unit:     "nm",
			expected: 60.0,
			tol:      0.5,
		},
		{
			name:     "Tortola to Virgin Gorda (approx 10nm)",
			lat1:     18.42,
			lon1:     -64.61,
			lat2:     18.45,
			lon2:     -64.44,
			unit:     "nm",
			expected: 9.8,
			tol:      0.5,
		},
		{
			name:     "KM check - London to Paris",
			lat1:     51.5074,
			lon1:     -0.1278,
			lat2:     48.8566,
			lon2:     2.3522,
			unit:     "km",
			expected: 344.0,
			tol:      1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := haversine(tt.lat1, tt.lon1, tt.lat2, tt.lon2, tt.unit)
			if math.Abs(got-tt.expected) > tt.tol {
				t.Errorf("haversine() = %v, want %v (tol %v)", got, tt.expected, tt.tol)
			}
		})
	}
}

func TestSearchBoundaryHint(t *testing.T) {
	tests := []struct {
		name      string
		lat       float64
		lng       float64
		radiusNM  float64
		expectedN string
		expectedS string
		expectedE string
		expectedW string
	}{
		{
			name:      "Dover / English Channel (Voyage 62)",
			lat:       50.9702,
			lng:       1.3578,
			radiusNM:  46,
			expectedN: "North edge (51.74°N, 1.36°E)",
			expectedS: "South edge (50.20°N, 1.36°E)",
			expectedE: "East edge (50.97°N, 2.58°E)",
			expectedW: "West edge (50.97°N, 0.14°E)",
		},
		{
			name:      "BVI / Caribbean (West Longitude)",
			lat:       18.45,
			lng:       -64.62,
			radiusNM:  30,
			expectedN: "North edge (18.95°N, 64.62°W)",
			expectedS: "South edge (17.95°N, 64.62°W)",
			expectedE: "East edge (18.45°N, 64.09°W)",
			expectedW: "West edge (18.45°N, 65.15°W)",
		},
		{
			name:      "Sydney / Southern Hemisphere",
			lat:       -33.86,
			lng:       151.20,
			radiusNM:  30,
			expectedN: "North edge (33.36°S, 151.20°E)",
			expectedS: "South edge (34.36°S, 151.20°E)",
			expectedE: "East edge (33.86°S, 151.80°E)",
			expectedW: "West edge (33.86°S, 150.60°E)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchBoundaryHint(tt.lat, tt.lng, tt.radiusNM)
			if !strings.Contains(got, tt.expectedN) {
				t.Errorf("searchBoundaryHint() missing north boundary %s, got: %s", tt.expectedN, got)
			}
			if !strings.Contains(got, tt.expectedS) {
				t.Errorf("searchBoundaryHint() missing south boundary %s, got: %s", tt.expectedS, got)
			}
			if !strings.Contains(got, tt.expectedE) {
				t.Errorf("searchBoundaryHint() missing east boundary %s, got: %s", tt.expectedE, got)
			}
			if !strings.Contains(got, tt.expectedW) {
				t.Errorf("searchBoundaryHint() missing west boundary %s, got: %s", tt.expectedW, got)
			}
		})
	}
}
