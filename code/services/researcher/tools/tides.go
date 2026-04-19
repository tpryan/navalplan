package tools

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/tpryan/niwago"
	"github.com/tpryan/noaago"
	"github.com/tpryan/uktidal"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	DefaultSearchRadius = 50
	MaxSearchRadius     = 150 // beyond this distance tide data is not locally meaningful
	MaxStationsToCheck  = 5

)

// TideArgs defines the arguments for the get_tides tool.
type TideArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

// TideEvent represents a single high or low tide event.
type TideEvent struct {
	Time   string  `json:"time"`
	Type   string  `json:"type"`
	Height float64 `json:"height_ft"`
	Unit   string  `json:"unit"`
}

// TideResult defines the response structure for the get_tides tool.
type TideResult struct {
	StationName   string      `json:"station_name"`
	StationID     string      `json:"station_id"`
	DistanceMiles float64     `json:"distance_miles"`
	Tides         []TideEvent `json:"tides"`
}

// RegionalTideProvider is implemented by each regional tide data source.
type RegionalTideProvider interface {
	CanHandle(lat, lng float64) bool
	GetTides(lat, lng float64, dateStr string) (TideResult, error)
}

// TideClient defines the interface for the NOAA API client.
type TideClient interface {
	FindStations(opts *noaago.StationOptions) (*noaago.StationResponse, error)
	GetTides(opts *noaago.TideOptions) (*noaago.TideResponse, error)
}

// UKTidalClient defines the interface for the ADMIRALTY UK Tidal API client.
type UKTidalClient interface {
	Stations(name string) (*uktidal.StationCollection, error)
	Events(stationId string, duration int) ([]uktidal.Event, error)
}

// NIWAClient defines the interface for the NIWA Tide Forecasting API client.
type NIWAClient interface {
	Fetch(p niwago.Params) (*niwago.Forecast, error)
}

// --- NOAAProvider ---

// NOAAProvider implements RegionalTideProvider using the NOAA CO-OPS API.
// It is registered last as the global fallback.
type NOAAProvider struct {
	client TideClient
}

func (p *NOAAProvider) CanHandle(lat, lng float64) bool {
	return true
}

func (p *NOAAProvider) GetTides(lat, lng float64, dateStr string) (TideResult, error) {
	stations, err := p.findNearbyStations(lat, lng)
	if err != nil {
		return TideResult{}, err
	}
	if len(stations) == 0 {
		return TideResult{}, ErrNotFound
	}

	var lastErr error
	for _, s := range stations {
		tides, err := p.fetchPredictions(s, dateStr)
		if err == nil {
			return TideResult{
				StationName:   s.Name,
				StationID:     s.ID,
				DistanceMiles: haversineDistanceMiles(lat, lng, s.Lat, s.Lng),
				Tides:         tides,
			}, nil
		}
		lastErr = err
	}
	return TideResult{}, fmt.Errorf("getting tides from nearby stations. Last error: %v", lastErr)
}

func (p *NOAAProvider) findNearbyStations(lat, lng float64) ([]noaago.Station, error) {
	for radius := DefaultSearchRadius; radius <= MaxSearchRadius; radius += DefaultSearchRadius {
		stationOpts := noaago.NewStationOptionsBuilder().
			Nearby(lat, lng, float64(radius)).
			Type(noaago.StationType("tidepredictions")).
			Build()

		stationsResp, err := p.client.FindStations(stationOpts)
		if err != nil {
			return nil, fmt.Errorf("%w: searching stations: %w", ErrAPIUnavailable, err)
		}

		if stationsResp.Count > 0 && len(stationsResp.Stations) > 0 {
			limit := MaxStationsToCheck
			if len(stationsResp.Stations) < limit {
				limit = len(stationsResp.Stations)
			}
			return stationsResp.Stations[:limit], nil
		}
	}
	return nil, nil
}

func (p *NOAAProvider) fetchPredictions(station noaago.Station, dateStr string) ([]TideEvent, error) {
	parsedDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, ErrInvalidDate
	}

	// Get for the date with a 48-hour buffer before and after.
	beginDate := parsedDate.Add(-48 * time.Hour)
	endDate := parsedDate.Add(48 * time.Hour)

	tideOpts := noaago.NewTideOptionsBuilder().
		StationID(station.ID).
		Product(noaago.ProductPredictions).
		Datum(noaago.DatumMLLW).
		Units(noaago.UnitsEnglish).
		Interval(noaago.IntervalHighLow).
		TimeZone(noaago.TimeZoneLSTLDT).
		DateRange(beginDate, endDate).
		Build()

	tideResp, err := p.client.GetTides(tideOpts)
	if err != nil {
		return nil, err
	}

	var events []TideEvent
	for _, pt := range tideResp.GetData() {
		val, _ := pt.ValueFloat()
		events = append(events, TideEvent{
			Time:   pt.Time,
			Type:   pt.Type,
			Height: val,
			Unit:   "ft",
		})
	}
	return events, nil
}

// --- UKProvider ---

// UKProvider implements RegionalTideProvider using the ADMIRALTY UK Tidal API.
type UKProvider struct {
	client UKTidalClient
}

const (
	ukLatMin = 49.5
	ukLatMax = 61.5
	ukLngMin = -11.0
	ukLngMax = 2.5
)

func (p *UKProvider) CanHandle(lat, lng float64) bool {
	return lat >= ukLatMin && lat <= ukLatMax && lng >= ukLngMin && lng <= ukLngMax
}

func (p *UKProvider) GetTides(lat, lng float64, dateStr string) (TideResult, error) {
	parsedDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return TideResult{}, ErrInvalidDate
	}

	stations, err := p.client.Stations("")
	if err != nil {
		return TideResult{}, fmt.Errorf("%w: listing UK stations: %w", ErrAPIUnavailable, err)
	}
	if len(stations.Features) == 0 {
		return TideResult{}, ErrNotFound
	}

	nearest, dist := nearestUKStation(lat, lng, stations.Features)

	events, err := p.client.Events(nearest.Properties.Id, 7)
	if err != nil {
		return TideResult{}, fmt.Errorf("%w: fetching UK tidal events: %w", ErrAPIUnavailable, err)
	}

	tides := filterUKEvents(events, parsedDate)

	return TideResult{
		StationName:   nearest.Properties.Name,
		StationID:     nearest.Properties.Id,
		DistanceMiles: dist,
		Tides:         tides,
	}, nil
}

func nearestUKStation(lat, lng float64, stations []uktidal.Station) (uktidal.Station, float64) {
	var nearest uktidal.Station
	minDist := math.MaxFloat64
	for _, s := range stations {
		if len(s.Geometry.Coordinates) < 2 {
			continue
		}
		// GeoJSON: coordinates are [longitude, latitude]
		sLng := s.Geometry.Coordinates[0]
		sLat := s.Geometry.Coordinates[1]
		d := haversineDistanceMiles(lat, lng, sLat, sLng)
		if d < minDist {
			minDist = d
			nearest = s
		}
	}
	return nearest, minDist
}

func filterUKEvents(events []uktidal.Event, date time.Time) []TideEvent {
	start := date.Add(-24 * time.Hour)
	end := date.Add(48 * time.Hour)

	var result []TideEvent
	for _, e := range events {
		t, err := parseUKDateTime(e.DateTime)
		if err != nil || t.Before(start) || t.After(end) {
			continue
		}

		tideType := "H"
		if e.EventType == "LowWater" {
			tideType = "L"
		}
		result = append(result, TideEvent{
			Time:   e.DateTime,
			Type:   tideType,
			Height: e.Height * metersToFeet,
			Unit:   "ft",
		})
	}
	return result
}

var ukDateFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z",
}

func parseUKDateTime(s string) (time.Time, error) {
	for _, f := range ukDateFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable datetime: %s", s)
}

// --- NIWAProvider ---

// NIWAProvider implements RegionalTideProvider using the NIWA Tide Forecasting API.
type NIWAProvider struct {
	client NIWAClient
}

const (
	nzLatMin = -47.5
	nzLatMax = -34.0
	nzLngMin = 165.0
	nzLngMax = 179.0
)

func (p *NIWAProvider) CanHandle(lat, lng float64) bool {
	return lat >= nzLatMin && lat <= nzLatMax && lng >= nzLngMin && lng <= nzLngMax
}

func (p *NIWAProvider) GetTides(lat, lng float64, dateStr string) (TideResult, error) {
	parsedDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return TideResult{}, ErrInvalidDate
	}

	// Start 1 day before to provide context around the target date.
	startDate := parsedDate.Add(-24 * time.Hour)

	forecast, err := p.client.Fetch(niwago.Params{
		Lat:          strconv.FormatFloat(lat, 'f', 6, 64),
		Long:         strconv.FormatFloat(lng, 'f', 6, 64),
		StartDate:    startDate,
		NumberOfDays: 3,
		Datum:        "LAT",
		Interval:     10,
	})
	if err != nil {
		return TideResult{}, fmt.Errorf("%w: fetching NIWA tides: %w", ErrAPIUnavailable, err)
	}

	tides := detectHighLow(forecast.Values)

	stationName := fmt.Sprintf("NIWA (%.4f, %.4f)", forecast.Metadata.Latitude, forecast.Metadata.Longitude)
	stationID := fmt.Sprintf("niwa_%.4f_%.4f", forecast.Metadata.Latitude, forecast.Metadata.Longitude)
	distMiles := haversineDistanceMiles(lat, lng, forecast.Metadata.Latitude, forecast.Metadata.Longitude)

	return TideResult{
		StationName:   stationName,
		StationID:     stationID,
		DistanceMiles: distMiles,
		Tides:         tides,
	}, nil
}

// detectHighLow identifies high and low tide events from a continuous series of
// tide height readings. It uses direction-change detection rather than simple
// 3-point comparison so plateau peaks/troughs are handled correctly: a flat top
// spanning multiple equal readings is treated as a single rising→falling transition.
func detectHighLow(values []niwago.Value) []TideEvent {
	if len(values) < 3 {
		return nil
	}

	// Assign direction for each step: +1 rising, -1 falling, 0 flat.
	// Then propagate the last non-zero direction through flat runs so that a
	// plateau at a peak is still seen as "still rising" until the next drop.
	dir := make([]int, len(values))
	for i := 1; i < len(values); i++ {
		switch {
		case values[i].Value > values[i-1].Value:
			dir[i] = 1
		case values[i].Value < values[i-1].Value:
			dir[i] = -1
		}
	}
	last := 0
	for i := range dir {
		if dir[i] == 0 {
			dir[i] = last
		} else {
			last = dir[i]
		}
	}

	var events []TideEvent
	for i := 1; i < len(values); i++ {
		switch {
		case dir[i-1] == 1 && dir[i] == -1: // rising→falling = high water
			events = append(events, TideEvent{
				Time:   values[i-1].Time,
				Type:   "H",
				Height: values[i-1].Value * metersToFeet,
				Unit:   "ft",
			})
		case dir[i-1] == -1 && dir[i] == 1: // falling→rising = low water
			events = append(events, TideEvent{
				Time:   values[i-1].Time,
				Type:   "L",
				Height: values[i-1].Value * metersToFeet,
				Unit:   "ft",
			})
		}
	}
	return events
}

// --- TideManager ---

// TideManager is the registry that routes tide requests to the correct regional provider.
type TideManager struct {
	providers []RegionalTideProvider
}

func (tm *TideManager) Close() error {
	return nil
}

// NewTideTool creates a new ADK tool for retrieving tide predictions.
// ukKey and niwaKey are optional; if empty the corresponding provider is skipped.
func NewTideTool(ukKey, niwaKey string) (tool.Tool, *TideManager, error) {
	var providers []RegionalTideProvider

	if ukKey != "" {
		providers = append(providers, &UKProvider{client: uktidal.NewClient(ukKey)})
	}
	if niwaKey != "" {
		providers = append(providers, &NIWAProvider{client: niwago.NewClient(niwaKey)})
	}
	// NOAA is always added last as the global fallback.
	providers = append(providers, &NOAAProvider{client: noaago.NewClient()})

	tm := &TideManager{providers: providers}

	t, err := functiontool.New(functiontool.Config{
		Name:        "get_tides",
		Description: "Retrieves high and low tide predictions for a specific date. Supports North America (NOAA), the UK and Ireland (ADMIRALTY), and New Zealand (NIWA).",
	}, tm.GetTides)
	return t, tm, err
}

func (tm *TideManager) GetTides(ctx tool.Context, args TideArgs) (TideResult, error) {
	for _, p := range tm.providers {
		if p.CanHandle(args.Latitude, args.Longitude) {
			return p.GetTides(args.Latitude, args.Longitude, args.Date)
		}
	}
	return TideResult{}, fmt.Errorf("no tidal data provider supports coordinates: %f, %f", args.Latitude, args.Longitude)
}

// haversineDistanceMiles returns the great-circle distance in miles between two
// lat/lng coordinates.
func haversineDistanceMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMi = 3958.8
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dPhi := (lat2 - lat1) * math.Pi / 180
	dLam := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLam/2)*math.Sin(dLam/2)
	return earthRadiusMi * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
