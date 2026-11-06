package gpx

import (
	"testing"
	"time"

	"app/internal/model"
)

func TestSplitTrackByDays(t *testing.T) {
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

	days := []model.VoyageDay{
		{ID: 10, StartLocationName: "Cowes", StartLatitude: 50.76, StartLongitude: -1.30, EndLocationName: "Newtown", EndLatitude: 50.73, EndLongitude: -1.40},
		{ID: 11, StartLocationName: "Newtown", StartLatitude: 50.73, StartLongitude: -1.40, EndLocationName: "Yarmouth", EndLatitude: 50.71, EndLongitude: -1.50},
	}

	tests := []struct {
		name      string
		track     ParsedTrack
		days      []model.VoyageDay
		wantLegs  int
		checkDest bool
	}{
		{
			name:      "split into 2 discrete legs matching 2 days",
			track:     master,
			days:      days,
			wantLegs:  2,
			checkDest: true,
		},
		{
			name:      "no days returns master unsplit",
			track:     master,
			days:      nil,
			wantLegs:  1,
			checkDest: false,
		},
		{
			name:      "no matching coordinates returns master",
			track:     master,
			days:      []model.VoyageDay{{ID: 99, StartLatitude: 10.0, StartLongitude: 10.0, EndLatitude: 12.0, EndLongitude: 12.0}},
			wantLegs:  1,
			checkDest: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			legs := SplitTrackByDays(tc.track, tc.days)
			if len(legs) != tc.wantLegs {
				t.Fatalf("SplitTrackByDays() got %d legs, want %d", len(legs), tc.wantLegs)
			}
			if tc.checkDest && len(legs) == 2 {
				if legs[0].DayID == nil || *legs[0].DayID != 10 {
					t.Errorf("Leg 1 DayID = %v, want 10", legs[0].DayID)
				}
				if legs[1].DayID == nil || *legs[1].DayID != 11 {
					t.Errorf("Leg 2 DayID = %v, want 11", legs[1].DayID)
				}
			}
		})
	}
}
