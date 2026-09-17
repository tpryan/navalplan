package gpx

import (
	"testing"
	"time"

	"app/internal/model"
)

func TestSplitTrackByStops(t *testing.T) {
	now := time.Now()
	t1 := now
	t2 := now.Add(1 * time.Hour)
	t3 := now.Add(2 * time.Hour)
	t4 := now.Add(3 * time.Hour)

	master := ParsedTrack{
		Name: "Full Solent Voyage",
		Kind: "recorded",
		Points: []Point{
			{Lat: 50.76, Lng: -1.30, Time: &t1}, // Cowes
			{Lat: 50.74, Lng: -1.35, Time: &t2},
			{Lat: 50.73, Lng: -1.40, Time: &t3}, // Newtown
			{Lat: 50.71, Lng: -1.50, Time: &t4}, // Yarmouth
		},
	}

	stops := []model.Stop{
		{ID: 10, LocationName: "Cowes", Latitude: 50.76, Longitude: -1.30},
		{ID: 11, LocationName: "Newtown", Latitude: 50.73, Longitude: -1.40},
		{ID: 12, LocationName: "Yarmouth", Latitude: 50.71, Longitude: -1.50},
	}

	tests := []struct {
		name      string
		track     ParsedTrack
		stops     []model.Stop
		wantLegs  int
		checkDest bool
	}{
		{
			name:      "split into 2 discrete legs matching 3 stops",
			track:     master,
			stops:     stops,
			wantLegs:  2,
			checkDest: true,
		},
		{
			name:      "single stop returns master unsplit",
			track:     master,
			stops:     stops[:1],
			wantLegs:  1,
			checkDest: false,
		},
		{
			name:      "no matching coordinates returns master",
			track:     master,
			stops:     []model.Stop{{ID: 99, Latitude: 10.0, Longitude: 10.0}, {ID: 100, Latitude: 12.0, Longitude: 12.0}},
			wantLegs:  1,
			checkDest: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			legs := SplitTrackByStops(tc.track, tc.stops)
			if len(legs) != tc.wantLegs {
				t.Fatalf("SplitTrackByStops() got %d legs, want %d", len(legs), tc.wantLegs)
			}
			if tc.checkDest && len(legs) == 2 {
				if legs[0].StopID == nil || *legs[0].StopID != 11 {
					t.Errorf("Leg 1 StopID = %v, want 11", legs[0].StopID)
				}
				if legs[1].StopID == nil || *legs[1].StopID != 12 {
					t.Errorf("Leg 2 StopID = %v, want 12", legs[1].StopID)
				}
			}
		})
	}
}
