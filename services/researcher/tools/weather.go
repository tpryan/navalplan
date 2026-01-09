package tools

import (
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/log"
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
	Date            string  `json:"date"`
	Condition       string  `json:"condition"`
	ForecastType    string  `json:"forecast_type"`
	MaxTemp         float64 `json:"max_temp"`
	MinTemp         float64 `json:"min_temp"`
	MaxWindKts      float64 `json:"max_wind_kts"`
	MaxGustsKts     float64 `json:"max_gusts_kts"`
	WindDirDeg      int     `json:"wind_dir_deg"`
	WindDirection   string  `json:"wind_direction"`
	PrecipTotal     float64 `json:"precip_total"`
	WaveHeight      float64 `json:"wave_height"`
	WaveDirection   float64 `json:"wave_direction"`
	WavePeriod      float64 `json:"wave_period"`
	DebugDurationMS int64   `json:"debug_duration_ms"`
	Error           string  `json:"error,omitempty"`
}

func NewWeatherTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_weather_forecast",
		Description: "Retrieves precise weather forecasts (Wind, Gusts, Temp, Waves) for a specific location and date.",
	}, func(ctx tool.Context, args WeatherArgs) (WeatherResult, error) {
		return GetWeatherForecast(args)
	})
}

func GetWeatherForecast(args WeatherArgs) (WeatherResult, error) {
	start := time.Now()
	log.Infof("tool:get_weather Fetching weather for %s at %f, %f", args.Date, args.Latitude, args.Longitude)
	// 1. Parse Inputs
	targetDate, err := time.Parse("2006-01-02", args.Date)
	if err != nil {
		return WeatherResult{Error: fmt.Sprintf("invalid date format: %v", err)}, nil
	}

	// 2. Auto-adjust for Future Dates
	// OpenMeteo forecast is valid for ~14 days.
	// If request is further out, use Seasonal forecast.
	isSeasonal := false
	daysUntil := time.Until(targetDate).Hours() / 24

	if daysUntil > 14 {
		isSeasonal = true
	}
	// 3. Initialize Client
	c := openmeteogo.NewClient()

	// 4. Build Options for Weather
	weatherOptsBuilder := openmeteogo.NewOptionsBuilder().
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
		})

	if isSeasonal {
		weatherOptsBuilder.Seasonal(true)
	}

	weatherOpts := weatherOptsBuilder.Build()

	// 5. Build Options for Marine
	// Marine forecasts generally don't extend to seasonal range in the standard API.
	// We will try anyway; if empty, we just handle it.
	marineOpts := openmeteogo.NewOptionsBuilder().
		Latitude(args.Latitude).
		Longitude(args.Longitude).
		Marine(true).
		Start(targetDate).
		End(targetDate).
		DailyMetrics(openmeteogo.Metrics{
			openmeteogo.WaveHeightMax,
			openmeteogo.WaveDirectionDominant,
			openmeteogo.WavePeriodMax,
		}).
		Build()

	// 6. Fetch Data (Parallel Weather and Marine)
	var weather, marine *openmeteogo.WeatherData
	var weatherErr, marineErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		weather, weatherErr = c.Get(weatherOpts)
	}()

	go func() {
		defer wg.Done()
		marine, marineErr = c.Get(marineOpts)
	}()

	wg.Wait()

	// Handle Weather Error
	if weatherErr != nil {
		fmt.Printf("OpenMeteo Weather Error: %v\n", weatherErr)
		return WeatherResult{Error: fmt.Sprintf("API Error (Weather): %v", weatherErr)}, nil
	}

	if weather == nil || weather.Daily.Time == nil || len(weather.Daily.Time) == 0 {
		fmt.Printf("OpenMeteo: No weather data for %s\n", args.Date)
		return WeatherResult{Error: "No weather data returned."}, nil
	}

	// 7. Process Marine Data
	// We treat marine errors as non-fatal (e.g. location might be on land, or date out of range)
	var waveHeight, waveDir, wavePeriod float64
	if marineErr == nil && marine != nil && marine.Daily.Time != nil && len(marine.Daily.Time) > 0 {
		if len(marine.Daily.WaveHeightMax) > 0 {
			waveHeight = marine.Daily.WaveHeightMax[0] * 3.28084 // Convert meters to feet
		}
		if len(marine.Daily.WaveDirectionDominant) > 0 {
			waveDir = marine.Daily.WaveDirectionDominant[0]
		}
		if len(marine.Daily.WavePeriodMax) > 0 {
			wavePeriod = marine.Daily.WavePeriodMax[0]
		}
	} else if marineErr != nil {
		// Don't log error for seasonal dates as it's expected to fail/be empty for marine
		if !isSeasonal {
			fmt.Printf("OpenMeteo Marine Error (ignoring): %v\n", marineErr)
		}
	}

	// Handle missing weather code
	condition := "Unknown weather"
	if len(weather.Daily.WeatherCode) > 0 {
		condition = openmeteogo.DescribeCode(int(weather.Daily.WeatherCode[0]))
	}

	forecastType := "Standard"
	if isSeasonal {
		forecastType = "Seasonal"
	}

	// Handle potentially missing metrics in Seasonal response
	var maxTemp, minTemp, maxWind, maxGusts, precip float64
	var windDir int

	if len(weather.Daily.Temperature2mMax) > 0 {
		maxTemp = weather.Daily.Temperature2mMax[0]
	}
	if len(weather.Daily.Temperature2mMin) > 0 {
		minTemp = weather.Daily.Temperature2mMin[0]
	}
	if len(weather.Daily.WindSpeed10mMax) > 0 {
		maxWind = weather.Daily.WindSpeed10mMax[0]
	}
	if len(weather.Daily.WindGusts10mMax) > 0 {
		maxGusts = weather.Daily.WindGusts10mMax[0]
	}
	if len(weather.Daily.WindDirection10mDominant) > 0 {
		windDir = weather.Daily.WindDirection10mDominant[0]
	}
	if len(weather.Daily.PrecipitationSum) > 0 {
		precip = weather.Daily.PrecipitationSum[0]
	}

	return WeatherResult{
		Date:            weather.Daily.Time[0],
		Condition:       condition,
		ForecastType:    forecastType,
		MaxTemp:         maxTemp,
		MinTemp:         minTemp,
		MaxWindKts:      maxWind,
		MaxGustsKts:     maxGusts,
		WindDirDeg:      windDir,
		WindDirection:   DegreesToDirection(float64(windDir)),
		PrecipTotal:     precip,
		WaveHeight:      waveHeight,
		WaveDirection:   waveDir,
		WavePeriod:      wavePeriod,
		DebugDurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func DegreesToDirection(deg float64) string {
	directions := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	index := int((deg + 11.25) / 22.5)
	return directions[index%16]
}
