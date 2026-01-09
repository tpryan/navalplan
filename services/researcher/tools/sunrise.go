package tools

import (
	"fmt"
	"math"
	"time"

	"github.com/charmbracelet/log"
	"github.com/nathan-osman/go-sunrise"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type SunriseArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

type SunriseResult struct {
	Date    string `json:"date"`
	Sunrise string `json:"sunrise"`
	Sunset  string `json:"sunset"`
	Error   string `json:"error,omitempty"`
}

func NewSunriseTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_sunrise_sunset",
		Description: "Retrieves sunrise and sunset times for a specific location and date.",
	}, func(ctx tool.Context, args SunriseArgs) (SunriseResult, error) {
		return GetSunriseSunset(args)
	})
}

func GetSunriseSunset(args SunriseArgs) (SunriseResult, error) {
	log.Infof("tool:get_sunrise Calculating sunrise/sunset for %s at %f, %f", args.Date, args.Latitude, args.Longitude)
	// 1. Parse Inputs
	targetDate, err := time.Parse("2006-01-02", args.Date)
	if err != nil {
		return SunriseResult{Error: fmt.Sprintf("invalid date format: %v", err)}, nil
	}

	// 2. Calculate Sunrise/Sunset (Returns UTC)
	rise, set := sunrise.SunriseSunset(
		args.Latitude,
		args.Longitude,
		targetDate.Year(),
		targetDate.Month(),
		targetDate.Day(),
	)

	// 3. Approximate Local Timezone (Nautical Time / Local Mean Time)
	// Offset = Round(Longitude / 15)
	offsetHours := int(math.Round(args.Longitude / 15.0))
	zone := time.FixedZone("LMT", offsetHours*3600)

	localRise := rise.In(zone)
	localSet := set.In(zone)

	// 4. Format Output
	// Return "YYYY-MM-DDTHH:mm:ss" without timezone offset.
	// The frontend will treat this as "local" time (wall-clock time),
	// effectively displaying the destination's time in the user's browser.
	timeFmt := "2006-01-02T15:04:05"

	return SunriseResult{
		Date:    args.Date,
		Sunrise: localRise.Format(timeFmt),
		Sunset:  localSet.Format(timeFmt),
	}, nil
}
