package tools

import (
	"fmt"
	"sync"
	"time"

	"github.com/tpryan/openmeteogo"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const metersToFeet = 3.28084

// WeatherArgs defines the arguments for the get_weather_forecast tool.
type WeatherArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

// WeatherResult defines the response structure for the get_weather_forecast tool.
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
}

// WeatherProvider implements the get_weather_forecast tool using the Open-Meteo API.
type WeatherProvider struct {
	client *openmeteogo.Client
}

// NewWeatherTool creates a new ADK tool for retrieving weather forecasts.
func NewWeatherTool() (tool.Tool, error) {
	wp := &WeatherProvider{
		client: openmeteogo.NewClient(),
	}
	return functiontool.New(functiontool.Config{
		Name:        "get_weather_forecast",
		Description: "Retrieves precise weather forecasts (Wind, Gusts, Temp, Waves) for a specific location and date.",
	}, wp.GetWeatherForecast)
}

func (wp *WeatherProvider) GetWeatherForecast(ctx tool.Context, args WeatherArgs) (WeatherResult, error) {
	start := time.Now()

	targetDate, err := time.Parse("2006-01-02", args.Date)
	if err != nil {
		return WeatherResult{}, fmt.Errorf("%w: %v", ErrInvalidDate, err)
	}

	// Auto-adjust for Future Dates
	isSeasonal := false
	daysUntil := time.Until(targetDate).Hours() / 24
	if daysUntil > 14 {
		isSeasonal = true
	}

	// Build Options
	weatherOpts := wp.buildWeatherOptions(args.Latitude, args.Longitude, targetDate, isSeasonal)
	marineOpts := wp.buildMarineOptions(args.Latitude, args.Longitude, targetDate)

	// Fetch Data (Parallel)
	var weather, marine *openmeteogo.WeatherData
	var weatherErr, marineErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		weather, weatherErr = wp.client.Get(weatherOpts)
	}()

	go func() {
		defer wg.Done()
		marine, marineErr = wp.client.Get(marineOpts)
	}()

	wg.Wait()

	if weatherErr != nil {
		return WeatherResult{}, fmt.Errorf("weather API error: %w", weatherErr)
	}

	if weather == nil || weather.Daily.Time == nil || len(weather.Daily.Time) == 0 {
		return WeatherResult{}, fmt.Errorf("no weather data returned for %s", args.Date)
	}

	result := wp.processResults(weather, marine, marineErr, isSeasonal)
	result.DebugDurationMS = time.Since(start).Milliseconds()

	return result, nil
}

func (wp *WeatherProvider) buildWeatherOptions(lat, lng float64, date time.Time, isSeasonal bool) *openmeteogo.Options {
	builder := openmeteogo.NewOptionsBuilder().
		Latitude(lat).
		Longitude(lng).
		TemperatureUnit(openmeteogo.Fahrenheit).
		WindspeedUnit(openmeteogo.KN).
		Start(date).
		End(date).
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
		builder.Seasonal(true)
	}
	return builder.Build()
}

func (wp *WeatherProvider) buildMarineOptions(lat, lng float64, date time.Time) *openmeteogo.Options {
	return openmeteogo.NewOptionsBuilder().
		Latitude(lat).
		Longitude(lng).
		Marine(true).
		Start(date).
		End(date).
		DailyMetrics(openmeteogo.Metrics{
			openmeteogo.WaveHeightMax,
			openmeteogo.WaveDirectionDominant,
			openmeteogo.WavePeriodMax,
		}).
		Build()
}

func (wp *WeatherProvider) processResults(weather, marine *openmeteogo.WeatherData, marineErr error, isSeasonal bool) WeatherResult {
	var waveHeight, waveDir, wavePeriod float64
	if marineErr == nil && marine != nil && marine.Daily.Time != nil && len(marine.Daily.Time) > 0 {
		if len(marine.Daily.WaveHeightMax) > 0 {
			waveHeight = marine.Daily.WaveHeightMax[0] * metersToFeet
		}
		if len(marine.Daily.WaveDirectionDominant) > 0 {
			waveDir = marine.Daily.WaveDirectionDominant[0]
		}
		if len(marine.Daily.WavePeriodMax) > 0 {
			wavePeriod = marine.Daily.WavePeriodMax[0]
		}
	}

	condition := "Unknown weather"
	if len(weather.Daily.WeatherCode) > 0 {
		condition = openmeteogo.DescribeCode(int(weather.Daily.WeatherCode[0]))
	}

	forecastType := "Standard"
	if isSeasonal {
		forecastType = "Seasonal"
	}

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
		Date:          weather.Daily.Time[0],
		Condition:     condition,
		ForecastType:  forecastType,
		MaxTemp:       maxTemp,
		MinTemp:       minTemp,
		MaxWindKts:    maxWind,
		MaxGustsKts:   maxGusts,
		WindDirDeg:    windDir,
		WindDirection: DegreesToDirection(float64(windDir)),
		PrecipTotal:   precip,
		WaveHeight:    waveHeight,
		WaveDirection: waveDir,
		WavePeriod:    wavePeriod,
	}
}

// DegreesToDirection converts a compass bearing in degrees to a cardinal direction string (e.g. "N", "NNE").
func DegreesToDirection(deg float64) string {
	directions := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	index := int((deg + 11.25) / 22.5)
	return directions[index%16]
}
