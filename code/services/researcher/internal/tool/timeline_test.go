package tool

import (
	"testing"

	"github.com/tpryan/noaago"
	"github.com/tpryan/openmeteogo"
)

func TestComputePassageTimeline(t *testing.T) {
	wp := &WeatherProvider{
		client: &mockWeatherClient{
			GetFunc: func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error) {
				res := &openmeteogo.WeatherData{}
				res.Daily.Temperature2mMax = []float64{75}
				res.Daily.Temperature2mMin = []float64{62}
				res.Hourly.WindSpeed10m = make([]float64, 24)
				res.Hourly.WindDirection10m = make([]int, 24)
				for i := range 24 {
					res.Hourly.WindSpeed10m[i] = 18.0
					res.Hourly.WindDirection10m[i] = 45 // NE wind opposing a NE course
				}
				return res, nil
			},
		},
	}

	tm := &TideManager{
		providers: []RegionalTideProvider{
			&NOAAProvider{
				client: &mockTideClient{
					FindStationsFunc: func(opts *noaago.StationOptions) (*noaago.StationResponse, error) {
						return &noaago.StationResponse{
							Count:    1,
							Stations: []noaago.Station{{ID: "8452660", Name: "Newport", Lat: 41.5, Lng: -71.3}},
						}, nil
					},
					GetTidesFunc: func(opts *noaago.TideOptions) (*noaago.TideResponse, error) {
						return &noaago.TideResponse{
							Data: []noaago.DataPoint{
								{Time: "2025-07-15 08:10", Value: "0.3", Type: "L"},
								{Time: "2025-07-15 14:25", Value: "4.2", Type: "H"},
							},
						}, nil
					},
				},
			},
		},
	}

	sp := &SunriseProvider{
		client: &mockTZClient{},
	}

	_, tp, err := NewTimelineTool(wp, tm, sp)
	if err != nil {
		t.Fatalf("NewTimelineTool() error = %v", err)
	}

	ctx, st := newStatefulMockContext()
	resp, err := tp.ComputeTimeline(ctx, TimelineRequest{
		FromLocation:  "Block Island",
		FromLatitude:  41.17,
		FromLongitude: -71.58,
		ToLocation:    "Cuttyhunk",
		ToLatitude:    41.42,
		ToLongitude:   -70.93,
		Date:          "2025-07-15",
	})
	if err != nil {
		t.Fatalf("ComputeTimeline() error = %v", err)
	}

	if resp.DistanceNM <= 0 {
		t.Errorf("expected positive DistanceNM, got %v", resp.DistanceNM)
	}
	if len(resp.Windows) != 4 {
		t.Fatalf("expected 4 candidate windows, got %d", len(resp.Windows))
	}
	if resp.RecommendedWindow == "" {
		t.Errorf("expected non-empty RecommendedWindow")
	}
	if _, err := st.Get(StateKeyPassageTimeline); err != nil {
		t.Errorf("expected timeline saved in session state, got error: %v", err)
	}
}
