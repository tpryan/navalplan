package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/nathan-osman/go-sunrise"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"googlemaps.github.io/maps"
)

// SunriseArgs defines the arguments for the get_sunrise_sunset tool.
type SunriseArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

// SunriseResult defines the response structure for the get_sunrise_sunset tool.
type SunriseResult struct {
	Date     string `json:"date"`
	Sunrise  string `json:"sunrise"`
	Sunset   string `json:"sunset"`
	TimeZone string `json:"time_zone"`
}

// TimezoneClient defines the interface for the Google Maps Timezone API client.
type TimezoneClient interface {
	Timezone(ctx context.Context, r *maps.TimezoneRequest) (*maps.TimezoneResult, error)
}

// SunriseProvider implements the get_sunrise_sunset tool.
type SunriseProvider struct {
	client TimezoneClient
}

// NewSunriseTool creates a new ADK tool for calculating sunrise and sunset times.
func NewSunriseTool(apiKey string) (tool.Tool, error) {
	c, err := maps.NewClient(maps.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("creating maps client: %w", err)
	}

	sp := &SunriseProvider{
		client: c,
	}
	return functiontool.New(functiontool.Config{
		Name:        "get_sunrise_sunset",
		Description: "Retrieves sunrise and sunset times for a specific location and date. Returns times in the location's local timezone.",
	}, sp.GetSunriseSunset)
}

func (sp *SunriseProvider) GetSunriseSunset(ctx tool.Context, args SunriseArgs) (SunriseResult, error) {
	targetDate, err := time.Parse("2006-01-02", args.Date)
	if err != nil {
		return SunriseResult{}, fmt.Errorf("%w: %v", ErrInvalidDate, err)
	}

	// 1. Calculate Sunrise/Sunset (Returns UTC)
	rise, set := sunrise.SunriseSunset(
		args.Latitude,
		args.Longitude,
		targetDate.Year(),
		targetDate.Month(),
		targetDate.Day(),
	)

	// 2. Fetch Actual Timezone
	tzReq := &maps.TimezoneRequest{
		Location:  &maps.LatLng{Lat: args.Latitude, Lng: args.Longitude},
		Timestamp: targetDate, // Use target date for correct DST
	}

	tzResult, err := sp.client.Timezone(context.Background(), tzReq)
	if err != nil {
		return SunriseResult{}, fmt.Errorf("timezone API error: %w", err)
	}

	// 3. Construct Time Location
	// The API returns DstOffset and RawOffset in seconds.
	// We can construct a fixed zone, or load the location if ID is standard.
	// Loading location by ID is safer for edge cases but requires local tzdata.
	// For simplicity and robustness without relying on local system tzdata for all world zones,
	// we will construct a FixedZone based on the total offset at that timestamp.

	totalOffsetSeconds := tzResult.DstOffset + tzResult.RawOffset
	loc := time.FixedZone(tzResult.TimeZoneName, totalOffsetSeconds)

	localRise := rise.In(loc)
	localSet := set.In(loc)

	timeFmt := "2006-01-02T15:04:05"

	return SunriseResult{
		Date:     args.Date,
		Sunrise:  localRise.Format(timeFmt),
		Sunset:   localSet.Format(timeFmt),
		TimeZone: tzResult.TimeZoneID,
	}, nil
}
