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

type WeatherResult struct {
	Date          string  `json:"date"`
	Summary       string  `json:"summary"`
	MaxTemp       float64 `json:"max_temp"`
	MinTemp       float64 `json:"min_temp"`
	MaxWindKts    float64 `json:"max_wind_kts"`
	MaxGustsKts   float64 `json:"max_gusts_kts"`
	WindDirDeg    int     `json:"wind_dir_deg"`
	PrecipTotal   float64 `json:"precip_total"`
	Error         string  `json:"error,omitempty"`
}

func NewWeatherTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_weather_forecast",
		Description: "Retrieves precise weather forecasts (Wind, Gusts, Temp) for a specific location and date.",
		}, func(ctx tool.Context, args WeatherArgs) (WeatherResult, error) {
			// 1. Parse Inputs
			targetDate, err := time.Parse("2006-01-02", args.Date)
			if err != nil {
				return WeatherResult{Error: fmt.Sprintf("invalid date format: %v", err)}, nil
			}
	
			// 2. Auto-adjust for Future Dates (Climate Estimate)
					// OpenMeteo forecast is valid for ~14 days.
					// If request is further out, shift to previous year to get historical data.
					isEstimate := false
					daysUntil := time.Until(targetDate).Hours() / 24
					
					if daysUntil > 14 {
						isEstimate = true
						// Shift back 1 year (or more if needed to be in past)
						// Simple logic: just -1 year for now
						targetDate = targetDate.AddDate(-1, 0, 0)
					}			// 3. Initialize Client
			c := openmeteogo.NewClient()
	
			// 4. Build Options
			opts := openmeteogo.NewOptionsBuilder().
				Latitude(args.Latitude).
				Longitude(args.Longitude).
				TemperatureUnit(openmeteogo.Fahrenheit).
				WindspeedUnit(openmeteogo.KN).
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
	
			// 5. Fetch Data
			weather, err := c.Get(opts)
			if err != nil {
				fmt.Printf("OpenMeteo Error: %v\n", err)
				return WeatherResult{Error: fmt.Sprintf("API Error: %v", err)}, nil
			}
	
			if weather == nil || weather.Daily.Time == nil || len(weather.Daily.Time) == 0 {
				fmt.Printf("OpenMeteo: No data for %s\n", args.Date)
				return WeatherResult{Error: "No weather data returned."}, nil
			}
	
			if len(weather.Daily.WeatherCode) == 0 {
				return WeatherResult{Error: "Weather code missing."}, nil
			}
			
			desc := openmeteogo.DescribeCode(int(weather.Daily.WeatherCode[0]))
			
			if isEstimate {
				desc = fmt.Sprintf("[Historical Estimate from %d] %s", targetDate.Year(), desc)
			}
	
			return WeatherResult{
				Date:          weather.Daily.Time[0],
				Summary:       desc,
				MaxTemp:       weather.Daily.Temperature2mMax[0],
				MinTemp:       weather.Daily.Temperature2mMin[0],
				MaxWindKts:    weather.Daily.WindSpeed10mMax[0],
				MaxGustsKts:   weather.Daily.WindGusts10mMax[0],
				WindDirDeg:    weather.Daily.WindDirection10mDominant[0],
				PrecipTotal:   weather.Daily.PrecipitationSum[0],
			}, nil
		})
	}
