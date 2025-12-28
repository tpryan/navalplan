package tools

import (
	"fmt"
	"time"

	"github.com/tpryan/openmeteogo"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type WeatherArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

type WeatherResult map[string]any

func NewWeatherTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_weather_forecast",
		Description: "Retrieves precise weather forecasts (Wind, Gusts, Temp) for a specific location and date.",
	}, func(ctx tool.Context, args WeatherArgs) (WeatherResult, error) {
		// 1. Parse Inputs
		targetDate, err := time.Parse("2006-01-02", args.Date)
		if err != nil {
			return nil, fmt.Errorf("invalid date format: %v", err)
		}

		// 2. Initialize Client
		c := openmeteogo.NewClient()

		// 3. Build Options
		opts := openmeteogo.NewOptionsBuilder().
			Latitude(args.Latitude).
			Longitude(args.Longitude).
			TemperatureUnit(openmeteogo.Fahrenheit).
			WindspeedUnit(openmeteogo.KN).
			// Timezone(*time.UTC). // Defaulting to auto/UTC often safer for daily stats
			Start(targetDate).
			End(targetDate).
			DailyMetrics(openmeteogo.Metrics{
				openmeteogo.WeatherCode,
				openmeteogo.Temperature2mMax,
				openmeteogo.Temperature2mMin,
				openmeteogo.WindSpeed10mMax,
				openmeteogo.WindGusts10mMax,
				openmeteogo.WindDirection10mDominant,
				openmeteogo.PrecipitationSum,
			}).
			Build()

		// 4. Fetch Data
		weather, err := c.Get(opts)
		if err != nil {
			fmt.Printf("OpenMeteo Error: %v\n", err)
			return WeatherResult{"error": fmt.Sprintf("API Error: %v. (Date might be out of 14-day forecast range)", err)}, nil
		}

		if len(weather.Daily.Time) == 0 {
			fmt.Printf("OpenMeteo: No data returned for %s\n", args.Date)
			return WeatherResult{"error": "No weather data returned. Date might be out of range."}, nil
		}

		// 5. Format Output
		desc := openmeteogo.DescribeCode(int(weather.Daily.WeatherCode[0]))

		return WeatherResult{
			"date":           weather.Daily.Time[0],
			"summary":        desc,
			"max_temp":       weather.Daily.Temperature2mMax[0],
			"min_temp":       weather.Daily.Temperature2mMin[0],
			"max_wind_kts":   weather.Daily.WindSpeed10mMax[0],
			"max_gusts_kts":  weather.Daily.WindGusts10mMax[0],
			"wind_dir_deg":   weather.Daily.WindDirection10mDominant[0],
			"precip_total":   weather.Daily.PrecipitationSum[0],
		}, nil
	})
}