package handlers

import (
	"math"
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
