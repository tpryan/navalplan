package tool

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/tpryan/niwago"
	"github.com/tpryan/noaago"
	"github.com/tpryan/uktidal"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
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

// ukForecastDays is the maximum number of days the ADMIRALTY Events API covers
// from today. The endpoint has no start-date parameter; it always begins now.
const ukForecastDays = 7

func (p *UKProvider) GetTides(lat, lng float64, dateStr string) (TideResult, error) {
	parsedDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return TideResult{}, ErrInvalidDate
	}

	// Reject dates beyond the API's fixed forecast window so TideManager can
	// fall through to the next provider rather than silently returning no tides.
	cutoff := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, ukForecastDays)
	if parsedDate.After(cutoff) {
		return TideResult{}, fmt.Errorf("UK tidal data unavailable for %s: ADMIRALTY API covers at most %d days from today", dateStr, ukForecastDays)
	}

	stations, err := p.client.Stations("")
	if err != nil {
		return TideResult{}, fmt.Errorf("%w: listing UK stations: %w", ErrAPIUnavailable, err)
	}
	if len(stations.Features) == 0 {
		return TideResult{}, ErrNotFound
	}

	candidates := nearestUKStations(lat, lng, stations.Features, MaxStationsToCheck)

	// Try candidates in order of distance. Secondary ports only publish High Water
	// predictions, so skip any station whose events don't contain both H and L types.
	for _, candidate := range candidates {
		events, err := p.client.Events(candidate.station.Properties.Id, 7)
		if err != nil {
			continue
		}
		tides := filterUKEvents(events, parsedDate)
		if hasBothTideTypes(tides) {
			return TideResult{
				StationName:   candidate.station.Properties.Name,
				StationID:     candidate.station.Properties.Id,
				DistanceMiles: candidate.dist,
				Tides:         tides,
			}, nil
		}
	}

	// Fallback: return whatever the nearest station has, even if incomplete.
	if len(candidates) > 0 {
		nearest := candidates[0]
		events, err := p.client.Events(nearest.station.Properties.Id, 7)
		if err != nil {
			return TideResult{}, fmt.Errorf("%w: fetching UK tidal events: %w", ErrAPIUnavailable, err)
		}
		return TideResult{
			StationName:   nearest.station.Properties.Name,
			StationID:     nearest.station.Properties.Id,
			DistanceMiles: nearest.dist,
			Tides:         filterUKEvents(events, parsedDate),
		}, nil
	}

	return TideResult{}, ErrNotFound
}

type ukStationDist struct {
	station uktidal.Station
	dist    float64
}

// nearestUKStations returns up to n stations sorted by distance from lat/lng.
func nearestUKStations(lat, lng float64, stations []uktidal.Station, n int) []ukStationDist {
	var all []ukStationDist
	for _, s := range stations {
		if len(s.Geometry.Coordinates) < 2 {
			continue
		}
		// GeoJSON: coordinates are [longitude, latitude]
		sLng := s.Geometry.Coordinates[0]
		sLat := s.Geometry.Coordinates[1]
		d := haversineDistanceMiles(lat, lng, sLat, sLng)
		all = append(all, ukStationDist{station: s, dist: d})
	}
	// Partial sort: bubble the n smallest distances to the front.
	for i := 0; i < n && i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].dist < all[i].dist {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if len(all) < n {
		return all
	}
	return all[:n]
}

// hasBothTideTypes returns true when tides contains at least one H and one L event.
func hasBothTideTypes(tides []TideEvent) bool {
	var hasH, hasL bool
	for _, t := range tides {
		switch t.Type {
		case "H":
			hasH = true
		case "L":
			hasL = true
		}
		if hasH && hasL {
			return true
		}
	}
	return false
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

func (tm *TideManager) GetTides(ctx agent.Context, args TideArgs) (TideResult, error) {
	var lastErr error
	for _, p := range tm.providers {
		if !p.CanHandle(args.Latitude, args.Longitude) {
			continue
		}
		result, err := p.GetTides(args.Latitude, args.Longitude, args.Date)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return TideResult{}, lastErr
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
