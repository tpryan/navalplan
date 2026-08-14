package tool

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Provider is anything backing a nautical tool that needs to release
// resources (HTTP clients, caches, etc.) when the service shuts down.
type Provider interface {
	Close() error
}

// NewNauticalService constructs a NauticalToolService along with the list of
// underlying Providers that must be closed on shutdown.
func NewNauticalService(ctx context.Context, mapsAPIKey, ukTidalAPIKey, niwaAPIKey string) (*NauticalToolService, []Provider, error) {
	var providers []Provider

	_, wp, err := NewWeatherTool()
	if err != nil {
		return nil, nil, fmt.Errorf("weather tool: %w", err)
	}
	providers = append(providers, wp)

	_, tp, err := NewTideTool(ukTidalAPIKey, niwaAPIKey)
	if err != nil {
		return nil, nil, fmt.Errorf("tide tool: %w", err)
	}
	providers = append(providers, tp)

	_, sp, err := NewSunriseTool(mapsAPIKey)
	if err != nil {
		return nil, nil, fmt.Errorf("sunrise tool: %w", err)
	}
	providers = append(providers, sp)

	_, pp, err := NewPlacesTool(ctx, mapsAPIKey)
	if err != nil {
		return nil, nil, fmt.Errorf("places tool: %w", err)
	}
	providers = append(providers, pp)

	return &NauticalToolService{
		Tides:   tp,
		Weather: wp,
		Sunrise: sp,
		Places:  pp,
	}, providers, nil
}

// AsTools wraps the NauticalToolService's methods as ADK function tools so
// they can be attached directly to an agent. Each tool is built independently
// so that a schema/reflection failure on one doesn't hide failures on the
// others: every error is collected and returned together.
func (s *NauticalToolService) AsTools() ([]tool.Tool, error) {
	var errs []error

	tideTool, err := functiontool.New(functiontool.Config{
		Name:        "GetTides",
		Description: "Queries hydrographic station databases for current and historic tidal matrices.",
	}, s.FetchTides)
	if err != nil {
		errs = append(errs, fmt.Errorf("GetTides: %w", err))
	}

	weatherTool, err := functiontool.New(functiontool.Config{
		Name:        "GetWeather",
		Description: "Fetches real-time NOAA offshore marine warnings and wind velocity vectors.",
	}, s.FetchWeather)
	if err != nil {
		errs = append(errs, fmt.Errorf("GetWeather: %w", err))
	}

	sunriseTool, err := functiontool.New(functiontool.Config{
		Name:        "GetSunriseSunset",
		Description: "Retrieves sunrise and sunset times for a specific location and date.",
	}, s.FetchSunriseSunset)
	if err != nil {
		errs = append(errs, fmt.Errorf("GetSunriseSunset: %w", err))
	}

	placesTool, err := functiontool.New(functiontool.Config{
		Name:        "FindPlacesNearby",
		Description: "Finds places (e.g. marinas, restaurants) near a location.",
	}, s.FetchPlacesNearby)
	if err != nil {
		errs = append(errs, fmt.Errorf("FindPlacesNearby: %w", err))
	}

	safetyTool, err := functiontool.New(functiontool.Config{
		Name:        "GetSafetyAlerts",
		Description: "Extracts active global navigational warnings and localized security alerts.",
	}, s.FetchSafetyAlerts)
	if err != nil {
		errs = append(errs, fmt.Errorf("GetSafetyAlerts: %w", err))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return []tool.Tool{tideTool, weatherTool, sunriseTool, placesTool, safetyTool}, nil
}
