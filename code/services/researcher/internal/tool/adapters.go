package tool

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"google.golang.org/adk/v2/agent"
)

// NauticalToolService aggregates the robust domain logic providers.
type NauticalToolService struct {
	Tides             *TideManager
	Weather           *WeatherProvider
	Sunrise           *SunriseProvider
	Places            *PlacesProvider
	SailingDirections *SailingDirectionsProvider
}

// TideRequest represents the input for the GetTides MCP tool.
type TideRequest struct {
	StationID string  `json:"station_id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Date      string  `json:"date"`
}

// McpTideEvent represents a single high or low tide event.
type McpTideEvent struct {
	Time     string  `json:"time"`
	Type     string  `json:"type"`
	HeightFt float64 `json:"height_ft"`
}

// McpTideResponse represents the output for the GetTides MCP tool.
type McpTideResponse struct {
	StationName string         `json:"station_name"`
	Events      []McpTideEvent `json:"events"`
}

// WeatherRequest represents the input for the GetWeather MCP tool.
type WeatherRequest struct {
	Coordinates string `json:"coordinates"` // Format: "lat,lng"
	Date        string `json:"date"`
}

// WeatherResponse represents the output for the GetWeather MCP tool.
type WeatherResponse struct {
	Summary          string    `json:"summary"`
	Condition        string    `json:"condition"`
	TempMinF         float64   `json:"temp_min_f"`
	TempMaxF         float64   `json:"temp_max_f"`
	WindSpeedKt      float64   `json:"wind_speed_kt"`
	WindDirection    string    `json:"wind_direction"`
	HourlyWind       []float64 `json:"hourly_wind"`
	HourlyWindDir    []string  `json:"hourly_wind_dir"`
	HourlyConditions []string  `json:"hourly_conditions"`
	HourlyTemp       []float64 `json:"hourly_temp"`
	HourlyGusts      []float64 `json:"hourly_gusts"`
	HourlyPrecip     []float64 `json:"hourly_precip"`
	HourlyWaveHeight []float64 `json:"hourly_wave_height"`
	HourlyWavePeriod []float64 `json:"hourly_wave_period"`
	HourlyWaveDir    []float64 `json:"hourly_wave_dir"`
	WaveHeightFt     float64   `json:"wave_height_ft"`
	DebugDurationMS  int64     `json:"debug_duration_ms"`
}

// SunriseRequest represents the input for the GetSunriseSunset MCP tool.
type SunriseRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Date      string  `json:"date"`
}

// SunriseResponse represents the output for the GetSunriseSunset MCP tool.
type SunriseResponse struct {
	Sunrise  string `json:"sunrise"`
	Sunset   string `json:"sunset"`
	TimeZone string `json:"time_zone"`
}

// PlacesRequest represents the input for the FindPlacesNearby MCP tool.
type PlacesRequest struct {
	Query     string  `json:"query"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Radius    float64 `json:"radius"`
}

// PlaceDetail represents a single place found by the search.
type PlaceDetail struct {
	Name            string  `json:"name"`
	Type            string  `json:"type"` // Mapped from query or business type
	Website         string  `json:"website"`
	Address         string  `json:"address"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	Rating          float64 `json:"rating"`
	UserRatingCount int32   `json:"user_rating_count"`
	BusinessStatus  string  `json:"business_status"`
	Details         struct {
		Description string `json:"description"`
		Protection  string `json:"protection"`
		VHF         string `json:"vhf"`
	} `json:"details"`
	References []string `json:"references"`
}

// McpPlacesResponse represents the output for the FindPlacesNearby MCP tool.
type McpPlacesResponse struct {
	Facilities []PlaceDetail `json:"facilities"`
}

// SafetyRequest represents the input for the GetSafetyAlerts MCP tool.
type SafetyRequest struct {
	Region string `json:"region"`
}

// SafetyResponse represents the output for the GetSafetyAlerts MCP tool.
type SafetyResponse struct {
	ActiveHazards []string `json:"active_hazards"`
	SecurityLevel string   `json:"security_level"`
}

// FetchTides routes execution flow to the TideManager.
func (s *NauticalToolService) FetchTides(ctx agent.Context, req TideRequest) (*McpTideResponse, error) {
	if s.Tides == nil {
		return nil, fmt.Errorf("tide service unavailable")
	}

	result, err := s.Tides.GetTides(ctx, TideArgs{
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Date:      req.Date,
	})
	if err != nil {
		return nil, err
	}

	res := &McpTideResponse{
		StationName: result.StationName,
	}

	for _, e := range result.Tides {
		res.Events = append(res.Events, McpTideEvent{
			Time:     e.Time,
			Type:     e.Type,
			HeightFt: e.Height,
		})
	}

	return res, nil
}

// FetchWeather fetches real-time weather data.
func (s *NauticalToolService) FetchWeather(ctx agent.Context, req WeatherRequest) (*WeatherResponse, error) {
	if s.Weather == nil {
		return nil, fmt.Errorf("weather service unavailable")
	}

	parts := strings.Split(req.Coordinates, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid coordinates format, expected 'lat,lng'")
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return nil, fmt.Errorf("invalid latitude: %v", err)
	}
	lng, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return nil, fmt.Errorf("invalid longitude: %v", err)
	}

	result, err := s.Weather.GetWeatherForecast(ctx, WeatherArgs{
		Latitude:  lat,
		Longitude: lng,
		Date:      req.Date,
	})
	if err != nil {
		return nil, err
	}

	return &WeatherResponse{
		Summary:          fmt.Sprintf("%s with max winds of %.1f kts", result.Condition, result.MaxWindKts),
		Condition:        result.Condition,
		TempMinF:         result.MinTemp,
		TempMaxF:         result.MaxTemp,
		WindSpeedKt:      result.MaxWindKts,
		WindDirection:    result.WindDirection,
		HourlyWind:       result.HourlyWind,
		HourlyWindDir:    result.HourlyWindDir,
		HourlyConditions: result.HourlyConditions,
		HourlyTemp:       result.HourlyTemp,
		HourlyGusts:      result.HourlyGusts,
		HourlyPrecip:     result.HourlyPrecip,
		HourlyWaveHeight: result.HourlyWaveHeight,
		HourlyWavePeriod: result.HourlyWavePeriod,
		HourlyWaveDir:    result.HourlyWaveDir,
		WaveHeightFt:     result.WaveHeight,
		DebugDurationMS:  result.DebugDurationMS,
	}, nil
}

// FetchSunriseSunset calculates solar phases.
func (s *NauticalToolService) FetchSunriseSunset(ctx agent.Context, req SunriseRequest) (*SunriseResponse, error) {
	if s.Sunrise == nil {
		return nil, fmt.Errorf("sunrise service unavailable")
	}

	result, err := s.Sunrise.GetSunriseSunset(ctx, SunriseArgs{
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Date:      req.Date,
	})
	if err != nil {
		return nil, err
	}

	// Format to HH:MM AM/PM as requested by Harbourmaster prompt
	parseAndFormat := func(tStr string) string {
		t, err := time.Parse("2006-01-02T15:04:05", tStr)
		if err != nil {
			return tStr
		}
		return t.Format("03:04 PM")
	}

	return &SunriseResponse{
		Sunrise:  parseAndFormat(result.Sunrise),
		Sunset:   parseAndFormat(result.Sunset),
		TimeZone: result.TimeZone,
	}, nil
}

// FetchPlacesNearby finds points of interest.
func (s *NauticalToolService) FetchPlacesNearby(ctx agent.Context, req PlacesRequest) (*McpPlacesResponse, error) {
	if s.Places == nil {
		return nil, fmt.Errorf("places service unavailable")
	}

	result, err := s.Places.FindPlaces(ctx, PlacesArgs{
		Query:     req.Query,
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Radius:    req.Radius,
	})
	if err != nil {
		slog.WarnContext(ctx, "Places search failed, continuing with empty places", "error", err, "query", req.Query)
		return &McpPlacesResponse{Facilities: nil}, nil
	}

	res := &McpPlacesResponse{}
	for _, p := range result.Places {
		detail := PlaceDetail{
			Name:            p.Name,
			Address:         p.Address,
			Latitude:        p.Latitude,
			Longitude:       p.Longitude,
			Rating:          p.Rating,
			UserRatingCount: p.UserRatingCount,
			BusinessStatus:  p.BusinessStatus,
			Website:         p.WebsiteURI,
		}

		// Try to map type from query
		q := strings.ToLower(req.Query)
		if strings.Contains(q, "anchorage") {
			detail.Type = "Anchorage"
		} else if strings.Contains(q, "marina") {
			detail.Type = "Marina"
		} else if strings.Contains(q, "mooring") {
			detail.Type = "Mooring"
		} else if strings.Contains(q, "fuel") {
			detail.Type = "Fuel Station"
		} else if strings.Contains(q, "restaurant") {
			detail.Type = "Restaurant"
		} else if strings.Contains(q, "bar") {
			detail.Type = "Bar"
		}

		res.Facilities = append(res.Facilities, detail)
	}

	return res, nil
}

// FetchSafetyAlerts extracts active global navigational warnings and localized security alerts.
func (s *NauticalToolService) FetchSafetyAlerts(ctx agent.Context, req SafetyRequest) (*SafetyResponse, error) {
	// Initially provides mock data as per geap.md recommendations as we don't have a safety tool yet.
	return &SafetyResponse{
		ActiveHazards: []string{"Shoaling reported near channel marker 4"},
		SecurityLevel: "Normal",
	}, nil
}

// SailingDirectionsRequest represents the input for the QuerySailingDirections MCP tool.
type SailingDirectionsRequest struct {
	Query     string `json:"query"`
	Territory string `json:"territory,omitempty"`
	TopK      int    `json:"top_k,omitempty"`
}

// CoastPilotRequest represents the input for the QueryCoastPilot MCP tool.
type CoastPilotRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

// SailingDirectionsResponse represents the output for the QuerySailingDirections MCP tool.
type SailingDirectionsResponse struct {
	Query           string `json:"query"`
	Territory       string `json:"territory"`
	Contexts        string `json:"contexts"`
	DebugDurationMS int64  `json:"debug_duration_ms"`
}

// FetchSailingDirections queries the hydrographic pilot RAG corpora.
func (s *NauticalToolService) FetchSailingDirections(ctx agent.Context, req SailingDirectionsRequest) (*SailingDirectionsResponse, error) {
	if s.SailingDirections == nil {
		return nil, fmt.Errorf("sailing directions service unavailable")
	}

	result, err := s.SailingDirections.QuerySailingDirections(ctx, SailingDirectionsArgs{
		Query:     req.Query,
		Territory: req.Territory,
		TopK:      req.TopK,
	})
	if err != nil {
		return nil, err
	}

	return &SailingDirectionsResponse{
		Query:           result.Query,
		Territory:       result.Territory,
		Contexts:        result.Contexts,
		DebugDurationMS: result.DebugDurationMS,
	}, nil
}

// FetchCoastPilot queries NOAA Coast Pilot volumes.
func (s *NauticalToolService) FetchCoastPilot(ctx agent.Context, req CoastPilotRequest) (*SailingDirectionsResponse, error) {
	if s.SailingDirections == nil {
		return nil, fmt.Errorf("coast pilot service unavailable")
	}

	result, err := s.SailingDirections.QueryCoastPilot(ctx, CoastPilotArgs{
		Query: req.Query,
		TopK:  req.TopK,
	})
	if err != nil {
		return nil, err
	}

	return &SailingDirectionsResponse{
		Query:           result.Query,
		Territory:       result.Territory,
		Contexts:        result.Contexts,
		DebugDurationMS: result.DebugDurationMS,
	}, nil
}
