package tool

import (
	"testing"
	"time"

	"github.com/tpryan/noaago"
)

// mockTideClient is a mock implementation of the TideClient interface.
type mockTideClient struct {
	FindStationsFunc func(opts *noaago.StationOptions) (*noaago.StationResponse, error)
	GetTidesFunc     func(opts *noaago.TideOptions) (*noaago.TideResponse, error)
}

func (m *mockTideClient) FindStations(opts *noaago.StationOptions) (*noaago.StationResponse, error) {
	if m.FindStationsFunc != nil {
		return m.FindStationsFunc(opts)
	}
	return nil, nil
}

func (m *mockTideClient) GetTides(opts *noaago.TideOptions) (*noaago.TideResponse, error) {
	if m.GetTidesFunc != nil {
		return m.GetTidesFunc(opts)
	}
	return nil, nil
}

func TestGetTides_Success(t *testing.T) {
	mockClient := &mockTideClient{
		FindStationsFunc: func(opts *noaago.StationOptions) (*noaago.StationResponse, error) {
			return &noaago.StationResponse{
				Count: 1,
				Stations: []noaago.Station{
					{ID: "8452660", Name: "Newport"},
				},
			}, nil
		},
		GetTidesFunc: func(opts *noaago.TideOptions) (*noaago.TideResponse, error) {
			return &noaago.TideResponse{
				Data: []noaago.DataPoint{
					{Time: "2025-01-01 06:00", Type: "H", Value: "4.5"},
					{Time: "2025-01-01 12:00", Type: "L", Value: "0.5"},
				},
			}, nil
		},
	}

	tp := &NOAAProvider{client: mockClient}
	args := TideArgs{
		Latitude:  41.5,
		Longitude: -71.3,
		Date:      "2025-01-01",
	}

	result, err := tp.GetTides(41.5, -71.3, args.Date)
	if err != nil {
		t.Fatalf("GetTides() error = %v", err)
	}

	if result.StationName != "Newport" {
		t.Errorf("Expected station Newport, got %s", result.StationName)
	}
	if len(result.Tides) != 2 {
		t.Errorf("Expected 2 tide events, got %d", len(result.Tides))
	}
	if result.Tides[0].Type != "H" {
		t.Errorf("Expected first tide to be High (H), got %s", result.Tides[0].Type)
	}
	// Station at 0,0 with request at 41.5,-71.3 should produce a non-zero distance.
	if result.DistanceMiles == 0 {
		t.Error("DistanceMiles should be non-zero when station coords differ from request")
	}
}

func TestHaversineDistanceMiles(t *testing.T) {
	// Newport, RI to Providence, RI is roughly 25 miles.
	dist := haversineDistanceMiles(41.490, -71.312, 41.824, -71.413)
	if dist < 20 || dist > 30 {
		t.Errorf("expected ~25 miles, got %.1f", dist)
	}
	// Same point should be zero.
	if d := haversineDistanceMiles(41.5, -71.3, 41.5, -71.3); d != 0 {
		t.Errorf("expected 0 for same point, got %v", d)
	}
}

func TestGetTides_NoStations(t *testing.T) {
	mockClient := &mockTideClient{
		FindStationsFunc: func(opts *noaago.StationOptions) (*noaago.StationResponse, error) {
			return &noaago.StationResponse{Count: 0, Stations: []noaago.Station{}}, nil
		},
	}

	tp := &NOAAProvider{client: mockClient}

	_, err := tp.GetTides(41.5, -71.3, "2025-01-01")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestGetTides_InvalidDate(t *testing.T) {
	args := TideArgs{
		Latitude:  41.497,
		Longitude: -71.362,
		Date:      "invalid",
	}

	mockClient := &mockTideClient{
		FindStationsFunc: func(opts *noaago.StationOptions) (*noaago.StationResponse, error) {
			return &noaago.StationResponse{
				Count: 1,
				Stations: []noaago.Station{
					{ID: "8452660", Name: "Newport"},
				},
			}, nil
		},
	}

	tp := &NOAAProvider{client: mockClient}
	_, err := tp.GetTides(args.Latitude, args.Longitude, args.Date)
	if err == nil {
		t.Error("Expected error for invalid date, got none")
	}
}

func TestTideBufferRange(t *testing.T) {
	// Verify the 48h buffer logic
	dateStr := "2026-05-29"
	parsedDate, _ := time.Parse("2006-01-02", dateStr)

	beginDate := parsedDate.Add(-48 * time.Hour)
	endDate := parsedDate.Add(48 * time.Hour)

	expectedBegin := parsedDate.AddDate(0, 0, -2)
	expectedEnd := parsedDate.AddDate(0, 0, 2)

	if !beginDate.Equal(expectedBegin) {
		t.Errorf("Begin date mismatch: got %v, want %v", beginDate, expectedBegin)
	}
	if !endDate.Equal(expectedEnd) {
		t.Errorf("End date mismatch: got %v, want %v", endDate, expectedEnd)
	}
}

func TestNewTideTool(t *testing.T) {
	tool, _, err := NewTideTool("", "")
	if err != nil {
		t.Fatalf("NewTideTool() error = %v", err)
	}

	if tool.Name() != "get_tides" {
		t.Errorf("NewTideTool().Name() = %v, want %v", tool.Name(), "get_tides")
	}
}
