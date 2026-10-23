package tool

import (
	"context"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/maps/places/apiv1/placespb"
	"github.com/googleapis/gax-go/v2"
	"github.com/tpryan/noaago"
	"github.com/tpryan/openmeteogo"
	"google.golang.org/genproto/googleapis/type/latlng"
	"google.golang.org/genproto/googleapis/type/localized_text"
	"googlemaps.github.io/maps"
)

type mockTZClient struct{}

func (m *mockTZClient) Timezone(ctx context.Context, r *maps.TimezoneRequest) (*maps.TimezoneResult, error) {
	return &maps.TimezoneResult{
		DstOffset:    -14400,
		RawOffset:    -18000,
		TimeZoneID:   "America/New_York",
		TimeZoneName: "EDT",
	}, nil
}

func TestStateBackedTools(t *testing.T) {
	ctx, st := newStatefulMockContext()

	// 1. Weather writes briefing_weather + digest
	wp := &WeatherProvider{
		client: &mockWeatherClient{
			GetFunc: func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error) {
				return &openmeteogo.WeatherData{
					Daily: openmeteogo.Daily{
						Time:                     []string{"2026-06-15"},
						WeatherCode:              []int{1},
						Temperature2mMax:         []float64{78},
						Temperature2mMin:         []float64{62},
						WindSpeed10mMax:          []float64{18},
						WindGusts10mMax:          []float64{24},
						WindDirection10mDominant: []int{225},
						PrecipitationSum:         []float64{0.12},
						WaveHeightMax:            []float64{1.2},
						WaveDirectionDominant:    []float64{210},
						WavePeriodMax:            []float64{7},
					},
					Hourly: openmeteogo.Hourly{
						WindSpeed10m:     []float64{8, 8, 9, 10, 10, 11, 12, 14, 15, 16, 17, 18, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 8},
						WindDirection10m: []int{180, 180, 180, 180, 180, 180, 180, 180, 180, 180, 270, 270, 270, 270, 270, 270, 270, 270, 270, 270, 270, 270, 270, 270},
						WeatherCode:      make([]int, 24),
						Precipitation:    []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.12, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
					},
				}, nil
			},
		},
	}
	targetDate := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	wRes, err := wp.GetWeatherForecast(ctx, WeatherArgs{Latitude: 41.49, Longitude: -71.31, Date: targetDate})
	if err != nil {
		t.Fatalf("GetWeatherForecast() error = %v", err)
	}
	if wRes.Digest == "" || wRes.SummaryDigest == "" {
		t.Errorf("expected non-empty weather Digest, got %q", wRes.Digest)
	}
	if !strings.Contains(wRes.Digest, "Wind shifts:") || !strings.Contains(wRes.Digest, "Precip hours:") {
		t.Errorf("expected weather digest to include wind shifts and precip hours, got %q", wRes.Digest)
	}
	storedWeather, err := st.Get(StateKeyBriefingWeather)
	if err != nil {
		t.Fatalf("expected %s in state, got error: %v", StateKeyBriefingWeather, err)
	}
	if sw, ok := storedWeather.(WeatherResult); !ok || len(sw.HourlyWind) != 24 {
		t.Errorf("unexpected stored weather: %+v", storedWeather)
	}

	// 2. Tides writes briefing_tides + digest
	tm := &TideManager{
		providers: []RegionalTideProvider{
			&NOAAProvider{
				client: &mockTideClient{
					FindStationsFunc: func(opts *noaago.StationOptions) (*noaago.StationResponse, error) {
						return &noaago.StationResponse{
							Count:    1,
							Stations: []noaago.Station{{ID: "8452660", Name: "Newport", Lat: 41.505, Lng: -71.326}},
						}, nil
					},
					GetTidesFunc: func(opts *noaago.TideOptions) (*noaago.TideResponse, error) {
						return &noaago.TideResponse{
							Data: []noaago.DataPoint{
								{Time: targetDate + " 06:15", Type: "L", Value: "0.4"},
								{Time: targetDate + " 12:30", Type: "H", Value: "3.9"},
							},
						}, nil
					},
				},
			},
		},
	}
	tRes, err := tm.GetTides(ctx, TideArgs{Latitude: 41.49, Longitude: -71.31, Date: targetDate})
	if err != nil {
		t.Fatalf("GetTides() error = %v", err)
	}
	if tRes.Digest == "" || !strings.Contains(tRes.Digest, "Range: 3.5 ft") {
		t.Errorf("expected tide digest with range, got %q", tRes.Digest)
	}
	if _, err := st.Get(StateKeyBriefingTides); err != nil {
		t.Fatalf("expected %s in state, got error: %v", StateKeyBriefingTides, err)
	}

	// 3. Sunrise writes briefing_sun + digest
	sp := &SunriseProvider{client: &mockTZClient{}}
	sRes, err := sp.GetSunriseSunset(ctx, SunriseArgs{Latitude: 41.49, Longitude: -71.31, Date: targetDate})
	if err != nil {
		t.Fatalf("GetSunriseSunset() error = %v", err)
	}
	if sRes.Digest == "" {
		t.Errorf("expected non-empty sunrise Digest")
	}
	if _, err := st.Get(StateKeyBriefingSun); err != nil {
		t.Fatalf("expected %s in state, got error: %v", StateKeyBriefingSun, err)
	}

	// 4. Places writes verified_places + SweepStopFacilities / SweepAreaInfrastructure
	pp := &PlacesProvider{
		client: &mockPlacesClient{
			SearchNearbyFunc: func(ctx context.Context, req *placespb.SearchNearbyRequest, opts ...gax.CallOption) (*placespb.SearchNearbyResponse, error) {
				return &placespb.SearchNearbyResponse{
					Places: []*placespb.Place{
						{
							DisplayName:      &localized_text.LocalizedText{Text: "Newport Yachting Center"},
							FormattedAddress: "4 Commercial Wharf, Newport, RI",
							Location:         &latlng.LatLng{Latitude: 41.488, Longitude: -71.315},
							Types:            []string{"marina"},
							GoogleMapsUri:    "https://maps.google.com/?cid=1",
						},
					},
				}, nil
			},
			SearchTextFunc: func(ctx context.Context, req *placespb.SearchTextRequest, opts ...gax.CallOption) (*placespb.SearchTextResponse, error) {
				return &placespb.SearchTextResponse{
					Places: []*placespb.Place{
						{
							DisplayName:      &localized_text.LocalizedText{Text: "Brenton Cove Anchorage"},
							FormattedAddress: "Newport Harbor, RI",
							Location:         &latlng.LatLng{Latitude: 41.478, Longitude: -71.325},
							GoogleMapsUri:    "https://maps.google.com/?cid=2",
						},
					},
				}, nil
			},
		},
	}
	sweepRes, err := pp.SweepStopFacilities(ctx, 41.49, -71.31, 5000)
	if err != nil {
		t.Fatalf("SweepStopFacilities() error = %v", err)
	}
	if len(sweepRes.Places) < 2 || sweepRes.Digest == "" {
		t.Errorf("expected deduplicated places and digest from SweepStopFacilities, got %d places, digest=%q", len(sweepRes.Places), sweepRes.Digest)
	}
	areaRes, err := pp.SweepAreaInfrastructure(ctx, 41.49, -71.31, 30000)
	if err != nil {
		t.Fatalf("SweepAreaInfrastructure() error = %v", err)
	}
	if len(areaRes.Places) < 2 {
		t.Errorf("expected deduplicated places from SweepAreaInfrastructure, got %d", len(areaRes.Places))
	}
	storedPlaces, err := st.Get(StateKeyVerifiedPlaces)
	if err != nil {
		t.Fatalf("expected %s in state, got error: %v", StateKeyVerifiedPlaces, err)
	}
	if spSlice, ok := storedPlaces.([]PlaceResult); !ok || len(spSlice) < 2 {
		t.Errorf("unexpected stored places: %+v", storedPlaces)
	}
}
