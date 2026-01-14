package tools

import (
	"fmt"
	"math"
	"time"

	"github.com/nathan-osman/go-sunrise"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// SunriseArgs defines the arguments for the get_sunrise_sunset tool.
type SunriseArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

// SunriseResult defines the response structure for the get_sunrise_sunset tool.
type SunriseResult struct {
	Date    string `json:"date"`
	Sunrise string `json:"sunrise"`
	Sunset  string `json:"sunset"`
}

// SunriseProvider implements the get_sunrise_sunset tool.
type SunriseProvider struct{}

// NewSunriseTool creates a new ADK tool for calculating sunrise and sunset times.
func NewSunriseTool() (tool.Tool, error) {
	sp := &SunriseProvider{}
	return functiontool.New(functiontool.Config{
		Name:        "get_sunrise_sunset",
		Description: "Retrieves sunrise and sunset times for a specific location and date.",
	}, sp.GetSunriseSunset)
}

func (sp *SunriseProvider) GetSunriseSunset(ctx tool.Context, args SunriseArgs) (SunriseResult, error) {
	targetDate, err := time.Parse("2006-01-02", args.Date)
	if err != nil {
		return SunriseResult{}, fmt.Errorf("%w: %v", ErrInvalidDate, err)
	}

	// Calculate Sunrise/Sunset (Returns UTC)
	rise, set := sunrise.SunriseSunset(
		args.Latitude,
		args.Longitude,
		targetDate.Year(),
		targetDate.Month(),
		targetDate.Day(),
	)

	// Approximate Local Timezone (Nautical Time / Local Mean Time)
	// Offset = Round(Longitude / 15)
	offsetHours := int(math.Round(args.Longitude / 15.0))
	zone := time.FixedZone("LMT", offsetHours*3600)

	localRise := rise.In(zone)
	localSet := set.In(zone)

	timeFmt := "2006-01-02T15:04:05"

	return SunriseResult{
		Date:    args.Date,
		Sunrise: localRise.Format(timeFmt),
		Sunset:  localSet.Format(timeFmt),
	}, nil
}
