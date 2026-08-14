package tool

import (
	"fmt"
	"testing"

	"github.com/tpryan/niwago"
	"github.com/tpryan/uktidal"
)

// --- mock clients ---

type mockUKTidalClient struct {
	StationsFunc func(name string) (*uktidal.StationCollection, error)
	EventsFunc   func(stationId string, duration int) ([]uktidal.Event, error)
}

func (m *mockUKTidalClient) Stations(name string) (*uktidal.StationCollection, error) {
	if m.StationsFunc != nil {
		return m.StationsFunc(name)
	}
	return &uktidal.StationCollection{}, nil
}

func (m *mockUKTidalClient) Events(stationId string, duration int) ([]uktidal.Event, error) {
	if m.EventsFunc != nil {
		return m.EventsFunc(stationId, duration)
	}
	return nil, nil
}

type mockNIWAClient struct {
	FetchFunc func(p niwago.Params) (*niwago.Forecast, error)
}

func (m *mockNIWAClient) Fetch(p niwago.Params) (*niwago.Forecast, error) {
	if m.FetchFunc != nil {
		return m.FetchFunc(p)
	}
	return &niwago.Forecast{}, nil
}

// --- CanHandle tests ---

func TestNOAAProvider_CanHandle(t *testing.T) {
	p := &NOAAProvider{}
	cases := []struct{ lat, lng float64 }{
		{41.5, -71.3},    // Rhode Island
		{51.5, -0.1},     // London
		{-36.85, 174.76}, // Auckland
		{0, 0},
	}
	for _, c := range cases {
		if !p.CanHandle(c.lat, c.lng) {
			t.Errorf("NOAAProvider.CanHandle(%v, %v) = false, want true (NOAA is global fallback)", c.lat, c.lng)
		}
	}
}

func TestUKProvider_CanHandle(t *testing.T) {
	p := &UKProvider{}
	cases := []struct {
		lat, lng float64
		want     bool
	}{
		{51.5, -0.1, true},      // London
		{55.8, -4.2, true},      // Glasgow
		{50.1, -5.5, true},      // Cornwall
		{41.5, -71.3, false},    // Rhode Island
		{-36.85, 174.76, false}, // Auckland
		{48.0, 2.3, false},      // Paris (just south of UK box)
	}
	for _, c := range cases {
		got := p.CanHandle(c.lat, c.lng)
		if got != c.want {
			t.Errorf("UKProvider.CanHandle(%v, %v) = %v, want %v", c.lat, c.lng, got, c.want)
		}
	}
}

func TestNIWAProvider_CanHandle(t *testing.T) {
	p := &NIWAProvider{}
	cases := []struct {
		lat, lng float64
		want     bool
	}{
		{-36.85, 174.76, true}, // Auckland
		{-41.28, 174.78, true}, // Wellington
		{-45.87, 170.5, true},  // Dunedin area
		{41.5, -71.3, false},   // Rhode Island
		{51.5, -0.1, false},    // London
		{-50.0, 170.0, false},  // south of NZ box
	}
	for _, c := range cases {
		got := p.CanHandle(c.lat, c.lng)
		if got != c.want {
			t.Errorf("NIWAProvider.CanHandle(%v, %v) = %v, want %v", c.lat, c.lng, got, c.want)
		}
	}
}

// --- UKProvider.GetTides ---

func TestUKProvider_GetTides_Success(t *testing.T) {
	mockClient := &mockUKTidalClient{
		StationsFunc: func(name string) (*uktidal.StationCollection, error) {
			return &uktidal.StationCollection{
				Features: []uktidal.Station{
					{
						Properties: uktidal.Properties{Id: "0001", Name: "Portsmouth"},
						Geometry:   uktidal.Geometry{Coordinates: []float64{-1.1, 50.8}},
					},
				},
			}, nil
		},
		EventsFunc: func(stationId string, duration int) ([]uktidal.Event, error) {
			return []uktidal.Event{
				{EventType: "HighWater", DateTime: "2026-04-18T06:30:00", Height: 1.5},
				{EventType: "LowWater", DateTime: "2026-04-18T12:45:00", Height: 0.3},
			}, nil
		},
	}

	p := &UKProvider{client: mockClient}
	result, err := p.GetTides(50.8, -1.1, "2026-04-18")
	if err != nil {
		t.Fatalf("GetTides() error = %v", err)
	}

	if result.StationName != "Portsmouth" {
		t.Errorf("expected station Portsmouth, got %s", result.StationName)
	}
	if len(result.Tides) != 2 {
		t.Errorf("expected 2 tide events, got %d", len(result.Tides))
	}
	if result.Tides[0].Type != "H" {
		t.Errorf("expected first event type H, got %s", result.Tides[0].Type)
	}
	if result.Tides[1].Type != "L" {
		t.Errorf("expected second event type L, got %s", result.Tides[1].Type)
	}
	// Heights should be converted from metres to feet.
	if result.Tides[0].Unit != "ft" {
		t.Errorf("expected unit ft, got %s", result.Tides[0].Unit)
	}
	expectedHigh := 1.5 * metersToFeet
	if result.Tides[0].Height < expectedHigh-0.01 || result.Tides[0].Height > expectedHigh+0.01 {
		t.Errorf("expected height ~%.2f ft, got %.2f", expectedHigh, result.Tides[0].Height)
	}
}

func TestUKProvider_GetTides_NoStations(t *testing.T) {
	mockClient := &mockUKTidalClient{
		StationsFunc: func(name string) (*uktidal.StationCollection, error) {
			return &uktidal.StationCollection{Features: []uktidal.Station{}}, nil
		},
	}

	p := &UKProvider{client: mockClient}
	_, err := p.GetTides(51.5, -0.1, "2026-04-18")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUKProvider_GetTides_InvalidDate(t *testing.T) {
	p := &UKProvider{client: &mockUKTidalClient{}}
	_, err := p.GetTides(51.5, -0.1, "not-a-date")
	if err == nil {
		t.Error("expected error for invalid date, got none")
	}
}

func TestUKProvider_GetTides_DateTooFar(t *testing.T) {
	// A date well beyond the 7-day ADMIRALTY window should return an error
	// rather than silently succeeding with empty tides.
	p := &UKProvider{client: &mockUKTidalClient{}}
	_, err := p.GetTides(51.5, -0.1, "2030-01-01")
	if err == nil {
		t.Error("expected error for date beyond ADMIRALTY 7-day window, got nil")
	}
}

// --- NIWAProvider.GetTides ---

func TestNIWAProvider_GetTides_Success(t *testing.T) {
	mockClient := &mockNIWAClient{
		FetchFunc: func(p niwago.Params) (*niwago.Forecast, error) {
			return &niwago.Forecast{
				Metadata: niwago.Metadata{Latitude: -36.85, Longitude: 174.76},
				Values: []niwago.Value{
					{Time: "2026-04-18 00:00", Value: 0.5},
					{Time: "2026-04-18 00:10", Value: 1.2},
					{Time: "2026-04-18 00:20", Value: 1.8}, // local max → H
					{Time: "2026-04-18 00:30", Value: 1.2},
					{Time: "2026-04-18 00:40", Value: 0.4}, // local min → L
					{Time: "2026-04-18 00:50", Value: 1.0},
					{Time: "2026-04-18 01:00", Value: 1.5},
				},
			}, nil
		},
	}

	p := &NIWAProvider{client: mockClient}
	result, err := p.GetTides(-36.85, 174.76, "2026-04-18")
	if err != nil {
		t.Fatalf("GetTides() error = %v", err)
	}

	if len(result.Tides) != 2 {
		t.Errorf("expected 2 H/L events, got %d", len(result.Tides))
	}
	if result.Tides[0].Type != "H" {
		t.Errorf("expected first event H, got %s", result.Tides[0].Type)
	}
	if result.Tides[1].Type != "L" {
		t.Errorf("expected second event L, got %s", result.Tides[1].Type)
	}
	if result.Tides[0].Unit != "ft" {
		t.Errorf("expected unit ft, got %s", result.Tides[0].Unit)
	}
}

func TestNIWAProvider_GetTides_InvalidDate(t *testing.T) {
	p := &NIWAProvider{client: &mockNIWAClient{}}
	_, err := p.GetTides(-36.85, 174.76, "not-a-date")
	if err == nil {
		t.Error("expected error for invalid date, got none")
	}
}

// --- TideManager dispatch ---

type captureProvider struct {
	canHandle bool
	called    bool
	returnErr error
}

func (c *captureProvider) CanHandle(lat, lng float64) bool { return c.canHandle }
func (c *captureProvider) GetTides(lat, lng float64, dateStr string) (TideResult, error) {
	c.called = true
	if c.returnErr != nil {
		return TideResult{}, c.returnErr
	}
	return TideResult{StationName: "captured"}, nil
}

func TestTideManager_DispatchesToFirstMatch(t *testing.T) {
	first := &captureProvider{canHandle: false}
	second := &captureProvider{canHandle: true}
	third := &captureProvider{canHandle: true}

	tm := &TideManager{providers: []RegionalTideProvider{first, second, third}}

	result, err := tm.GetTides(newMockContext(), TideArgs{Latitude: 51.5, Longitude: -0.1, Date: "2026-04-18"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StationName != "captured" {
		t.Errorf("unexpected station: %s", result.StationName)
	}
	if first.called {
		t.Error("first provider should not have been called (CanHandle=false)")
	}
	if !second.called {
		t.Error("second provider should have been called")
	}
	if third.called {
		t.Error("third provider should not have been called (second matched first)")
	}
}

func TestTideManager_FallsThroughOnError(t *testing.T) {
	// First matching provider errors; TideManager should try the next one.
	failing := &captureProvider{canHandle: true}
	succeeding := &captureProvider{canHandle: true}

	tm := &TideManager{providers: []RegionalTideProvider{failing, succeeding}}

	// Make failing return an error.
	failing.returnErr = fmt.Errorf("date out of range")

	result, err := tm.GetTides(newMockContext(), TideArgs{Latitude: 51.5, Longitude: -0.1, Date: "2030-01-01"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StationName != "captured" {
		t.Errorf("unexpected station: %s", result.StationName)
	}
	if !failing.called {
		t.Error("failing provider should have been called")
	}
	if !succeeding.called {
		t.Error("succeeding provider should have been called after fallthrough")
	}
}

func TestTideManager_NoProviderMatch(t *testing.T) {
	tm := &TideManager{providers: []RegionalTideProvider{
		&captureProvider{canHandle: false},
	}}

	_, err := tm.GetTides(newMockContext(), TideArgs{Latitude: 0, Longitude: 0, Date: "2026-04-18"})
	if err == nil {
		t.Error("expected error when no provider matches")
	}
}

// --- detectHighLow ---

func TestDetectHighLow(t *testing.T) {
	values := []niwago.Value{
		{Time: "t0", Value: 1.0},
		{Time: "t1", Value: 2.0}, // max
		{Time: "t2", Value: 1.5},
		{Time: "t3", Value: 0.5}, // min
		{Time: "t4", Value: 1.0},
	}
	events := detectHighLow(values)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "H" {
		t.Errorf("expected H, got %s", events[0].Type)
	}
	if events[1].Type != "L" {
		t.Errorf("expected L, got %s", events[1].Type)
	}
}

func TestDetectHighLow_Plateau(t *testing.T) {
	// Plateau at peak: the maximum spans multiple equal readings.
	// The old 3-point algorithm missed this; the direction-change algorithm must catch it.
	values := []niwago.Value{
		{Time: "t0", Value: 1.0},
		{Time: "t1", Value: 2.0},
		{Time: "t2", Value: 2.0}, // plateau top — equal to prev, not strictly greater than next
		{Time: "t3", Value: 2.0},
		{Time: "t4", Value: 1.5},
		{Time: "t5", Value: 0.5}, // trough
		{Time: "t6", Value: 1.0},
	}
	events := detectHighLow(values)
	if len(events) != 2 {
		t.Fatalf("expected 2 events for plateau peak, got %d", len(events))
	}
	if events[0].Type != "H" {
		t.Errorf("expected H, got %s", events[0].Type)
	}
	if events[1].Type != "L" {
		t.Errorf("expected L, got %s", events[1].Type)
	}
}

func TestDetectHighLow_TooFewValues(t *testing.T) {
	if events := detectHighLow(nil); events != nil {
		t.Errorf("expected nil for empty input, got %v", events)
	}
	if events := detectHighLow([]niwago.Value{{}, {}}); events != nil {
		t.Errorf("expected nil for 2 values, got %v", events)
	}
}

// --- nearestUKStation ---

func TestNearestUKStations(t *testing.T) {
	stations := []uktidal.Station{
		{
			Properties: uktidal.Properties{Id: "far", Name: "Far Station"},
			Geometry:   uktidal.Geometry{Coordinates: []float64{0.0, 55.0}}, // [lng, lat]
		},
		{
			Properties: uktidal.Properties{Id: "near", Name: "Near Station"},
			Geometry:   uktidal.Geometry{Coordinates: []float64{-1.1, 50.8}},
		},
		{
			Properties: uktidal.Properties{Id: "mid", Name: "Mid Station"},
			Geometry:   uktidal.Geometry{Coordinates: []float64{-0.5, 52.0}},
		},
	}

	result := nearestUKStations(50.8, -1.1, stations, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result))
	}
	if result[0].station.Properties.Id != "near" {
		t.Errorf("expected near station first, got %s", result[0].station.Properties.Id)
	}
	if result[0].dist > 1.0 {
		t.Errorf("expected near-zero distance, got %.2f miles", result[0].dist)
	}
}

func TestHasBothTideTypes(t *testing.T) {
	cases := []struct {
		tides []TideEvent
		want  bool
	}{
		{[]TideEvent{{Type: "H"}, {Type: "L"}}, true},
		{[]TideEvent{{Type: "H"}, {Type: "H"}}, false},
		{[]TideEvent{{Type: "L"}}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := hasBothTideTypes(c.tides); got != c.want {
			t.Errorf("hasBothTideTypes(%v) = %v, want %v", c.tides, got, c.want)
		}
	}
}

func TestUKProvider_SkipsHWOnlyStation(t *testing.T) {
	// First (nearest) station returns only HighWater events → should be skipped.
	// Second station returns both → should be used.
	callCount := 0
	mockClient := &mockUKTidalClient{
		StationsFunc: func(name string) (*uktidal.StationCollection, error) {
			return &uktidal.StationCollection{
				Features: []uktidal.Station{
					{
						Properties: uktidal.Properties{Id: "hw-only", Name: "HW Only"},
						Geometry:   uktidal.Geometry{Coordinates: []float64{-1.1, 50.8}},
					},
					{
						Properties: uktidal.Properties{Id: "full", Name: "Full Port"},
						Geometry:   uktidal.Geometry{Coordinates: []float64{-1.2, 50.9}},
					},
				},
			}, nil
		},
		EventsFunc: func(stationId string, duration int) ([]uktidal.Event, error) {
			callCount++
			if stationId == "hw-only" {
				return []uktidal.Event{
					{EventType: "HighWater", DateTime: "2026-04-20T06:30:00", Height: 11.2},
				}, nil
			}
			return []uktidal.Event{
				{EventType: "HighWater", DateTime: "2026-04-20T06:30:00", Height: 11.2},
				{EventType: "LowWater", DateTime: "2026-04-20T12:45:00", Height: 1.0},
			}, nil
		},
	}

	p := &UKProvider{client: mockClient}
	result, err := p.GetTides(50.8, -1.1, "2026-04-20")
	if err != nil {
		t.Fatalf("GetTides() error = %v", err)
	}
	if result.StationName != "Full Port" {
		t.Errorf("expected Full Port, got %s", result.StationName)
	}
	if callCount < 2 {
		t.Errorf("expected Events to be called at least twice (once per station tried), got %d", callCount)
	}
}
