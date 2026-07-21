package tools

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/adk/tool"
)

// NauticalToolService aggregates the robust domain logic providers.
type NauticalToolService struct {
	Tides   *TideManager
	Weather *WeatherProvider
}

// TideRequest represents the input for the GetTides MCP tool.
type TideRequest struct {
	StationID string  `json:"station_id"` // Note: In this adapter we map StationID to Lat/Lng for legacy support if needed
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Date      string  `json:"date"`
}

// TideResponse represents the output for the GetTides MCP tool.
type TideResponse struct {
	StationName  string    `json:"station_name"`
	HighTide     string    `json:"high_tide"`
	LowTide      string    `json:"low_tide"`
	Measurements []float64 `json:"measurements"`
}

// WeatherRequest represents the input for the GetWeather MCP tool.
type WeatherRequest struct {
	Coordinates string `json:"coordinates"` // Format: "lat,lng"
	Date        string `json:"date"`
}

// WeatherResponse represents the output for the GetWeather MCP tool.
type WeatherResponse struct {
	WindSpeed float64  `json:"wind_speed"`
	Direction string   `json:"direction"`
	Warnings  []string `json:"warnings"`
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
func (s *NauticalToolService) FetchTides(ctx tool.Context, req TideRequest) (*TideResponse, error) {
	if s.Tides == nil {
		return nil, fmt.Errorf("tide service unavailable")
	}

	// For now, we use the Lat/Lng from the request as the legacy TideManager expects it.
	// In the future, we could resolve StationID to Lat/Lng.
	result, err := s.Tides.GetTides(nil, TideArgs{
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Date:      req.Date,
	})
	if err != nil {
		return nil, err
	}

	res := &TideResponse{
		StationName: result.StationName,
	}

	for _, e := range result.Tides {
		if e.Type == "H" && res.HighTide == "" {
			res.HighTide = e.Time
		}
		if e.Type == "L" && res.LowTide == "" {
			res.LowTide = e.Time
		}
		res.Measurements = append(res.Measurements, e.Height)
	}

	return res, nil
}

// FetchWeather fetches real-time weather data.
func (s *NauticalToolService) FetchWeather(ctx tool.Context, req WeatherRequest) (*WeatherResponse, error) {
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

	result, err := s.Weather.GetWeatherForecast(nil, WeatherArgs{
		Latitude:  lat,
		Longitude: lng,
		Date:      req.Date,
	})
	if err != nil {
		return nil, err
	}

	return &WeatherResponse{
		WindSpeed: result.MaxWindKts,
		Direction: result.WindDirection,
		Warnings:  result.HourlyConditions, // Using conditions as warnings for now
	}, nil
}

// FetchSafetyAlerts extracts active global navigational warnings and localized security alerts.
func (s *NauticalToolService) FetchSafetyAlerts(ctx tool.Context, req SafetyRequest) (*SafetyResponse, error) {
	// Initially provides mock data as per geap.md recommendations as we don't have a safety tool yet.
	return &SafetyResponse{
		ActiveHazards: []string{"Shoaling reported near channel marker 4"},
		SecurityLevel: "Normal",
	}, nil
}
