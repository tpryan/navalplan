package handlers

import "testing"

// facsAt returns n facilities due north of (0,0) at distNM nautical miles.
func facsAt(n int, distNM float64) []Facility {
	var out []Facility
	for i := 0; i < n; i++ {
		out = append(out, Facility{Name: "f", Latitude: distNM / 60.0, Longitude: 0})
	}
	return out
}

func TestNearbyFacilities(t *testing.T) {
	tests := []struct {
		name string
		facs []Facility
		max  float64
		want int
	}{
		{"walkable cluster wins over denser cluster farther out",
			append(facsAt(8, 0.4), facsAt(30, 1.8)...), 5, 8},
		{"one nearby place does not hide the rest",
			append(facsAt(1, 0.3), facsAt(10, 2.0)...), 5, 11},
		{"sparse stop shows everything within the stop radius",
			append(facsAt(2, 0.3), facsAt(3, 4.0)...), 5, 5},
		{"nothing beyond the stop radius",
			append(facsAt(3, 1.0), facsAt(20, 8.0)...), 5, 3},
		{"coordless facilities are always kept",
			append(facsAt(8, 0.4), Facility{Name: "no coords"}), 5, 9},
	}
	for _, tc := range tests {
		if got := len(nearbyFacilities(tc.facs, 0, 0, tc.max)); got != tc.want {
			t.Errorf("%s: got %d facilities, want %d", tc.name, got, tc.want)
		}
	}
}
