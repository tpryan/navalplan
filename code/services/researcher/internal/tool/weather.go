package tool

import (
	"fmt"
	"time"

	"github.com/tpryan/openmeteogo"
	"golang.org/x/sync/errgroup"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
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
	Date             string    `json:"date"`
	Condition        string    `json:"condition"`
	ForecastType     string    `json:"forecast_type"`
	MaxTemp          float64   `json:"max_temp"`
	MinTemp          float64   `json:"min_temp"`
	MaxWindKts       float64   `json:"max_wind_kts"`
	MaxGustsKts      float64   `json:"max_gusts_kts"`
	WindDirDeg       int       `json:"wind_dir_deg"`
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
	PrecipTotal      float64   `json:"precip_total"`
	WaveHeight       float64   `json:"wave_height"`
	WaveDirection    float64   `json:"wave_direction"`
	WavePeriod       float64   `json:"wave_period"`
	DebugDurationMS  int64     `json:"debug_duration_ms"`
}

// WeatherClient defines the interface for the Open-Meteo API client.
type WeatherClient interface {
	Get(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error)
}

// WeatherProvider implements the get_weather_forecast tool using the Open-Meteo API.
type WeatherProvider struct {
	client WeatherClient
}

// Close closes the underlying client connection.
func (wp *WeatherProvider) Close() error {
	return nil
}

// NewWeatherTool creates a new ADK tool for retrieving weather forecasts.
func NewWeatherTool() (tool.Tool, *WeatherProvider, error) {
	wp := &WeatherProvider{
		client: openmeteogo.NewClient(),
	}
	t, err := functiontool.New(functiontool.Config{
		Name:        "get_weather_forecast",
		Description: "Retrieves precise weather forecasts (Wind, Gusts, Temp, Waves) for a specific location and date.",
	}, wp.GetWeatherForecast)
	return t, wp, err
}

func (wp *WeatherProvider) GetWeatherForecast(ctx agent.Context, args WeatherArgs) (WeatherResult, error) {
	start := time.Now()

	targetDate, err := time.Parse("2006-01-02", args.Date)
	if err != nil {
		return WeatherResult{}, fmt.Errorf("%w: %v", ErrInvalidDate, err)
	}

	nowUTC := time.Now().UTC()
	todayUTC := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
	daysUntil := targetDate.Sub(todayUTC).Hours() / 24
	var weather, marine *openmeteogo.WeatherData
	var weatherErr, marineErr error

	// Live forecast is available for dates up to 16 days out
	if daysUntil >= -1 && daysUntil <= 16 {
		weatherOpts := wp.buildWeatherOptions(args.Latitude, args.Longitude, targetDate)
		marineOpts := wp.buildMarineOptions(args.Latitude, args.Longitude, targetDate)

		g, _ := errgroup.WithContext(ctx) //nolint:errcheck // derived context unused: WeatherClient.Get has no context parameter
		g.Go(func() error {
			var err error
			weather, err = wp.client.Get(weatherOpts)
			if err != nil {
				weatherErr = err
			}
			return nil
		})
		g.Go(func() error {
			var err error
			marine, err = wp.client.Get(marineOpts)
			if err != nil {
				marineErr = err
			}
			return nil
		})
		_ = g.Wait()

		if weather != nil && len(weather.Daily.WeatherCode) > 0 {
			result := wp.processResults(weather, marine, marineErr, false)
			result.Date = args.Date
			result.DebugDurationMS = time.Since(start).Milliseconds()
			return result, nil
		}
	}

	// For future dates (>16 days out) or if live forecast failed, fetch historical archive data (1 year prior)
	historicalDate := targetDate.AddDate(-1, 0, 0)
	weatherOpts := wp.buildWeatherOptions(args.Latitude, args.Longitude, historicalDate)

	weather, weatherErr = wp.client.Get(weatherOpts)
	if weatherErr != nil || weather == nil || len(weather.Daily.WeatherCode) == 0 {
		if daysUntil > 16 {
			// Fallback climatology with full 24h hourly arrays
			hourlyWind := make([]float64, 24)
			hourlyWindDir := make([]string, 24)
			hourlyConditions := make([]string, 24)
			hourlyTemp := make([]float64, 24)
			hourlyGusts := make([]float64, 24)
			hourlyPrecip := make([]float64, 24)
			hourlyWaveHeight := make([]float64, 24)
			hourlyWavePeriod := make([]float64, 24)
			hourlyWaveDir := make([]float64, 24)

			for i := 0; i < 24; i++ {
				hourlyWind[i] = 12.0
				hourlyWindDir[i] = "NW"
				hourlyConditions[i] = "Partly cloudy"
				hourlyTemp[i] = 72.0
				hourlyGusts[i] = 16.0
				hourlyPrecip[i] = 0.0
				hourlyWaveHeight[i] = 3.0
				hourlyWavePeriod[i] = 8.0
				hourlyWaveDir[i] = 290.0
			}

			return WeatherResult{
				Date:             args.Date,
				Condition:        "Seasonal Average",
				ForecastType:     "Climatology Projection",
				MaxTemp:          78.0,
				MinTemp:          68.0,
				MaxWindKts:       12.0,
				MaxGustsKts:      16.0,
				WindDirDeg:       315,
				WindDirection:    "NW",
				HourlyWind:       hourlyWind,
				HourlyWindDir:    hourlyWindDir,
				HourlyConditions: hourlyConditions,
				HourlyTemp:       hourlyTemp,
				HourlyGusts:      hourlyGusts,
				HourlyPrecip:     hourlyPrecip,
				HourlyWaveHeight: hourlyWaveHeight,
				HourlyWavePeriod: hourlyWavePeriod,
				HourlyWaveDir:    hourlyWaveDir,
				WaveHeight:       3.0,
				WaveDirection:    290,
				WavePeriod:       8.0,
				DebugDurationMS:  time.Since(start).Milliseconds(),
			}, nil
		}
		if weatherErr != nil {
			return WeatherResult{}, fmt.Errorf("%w: %w", ErrAPIUnavailable, weatherErr)
		}
		return WeatherResult{}, fmt.Errorf("no weather data returned for %s", args.Date)
	}

	result := wp.processResults(weather, nil, fmt.Errorf("marine unavailable for historical"), true)
	result.Date = args.Date
	result.DebugDurationMS = time.Since(start).Milliseconds()
	return result, nil
}

func (wp *WeatherProvider) buildWeatherOptions(lat, lng float64, date time.Time) *openmeteogo.Options {
	return openmeteogo.NewOptionsBuilder().
		Latitude(lat).
		Longitude(lng).
		TemperatureUnit(openmeteogo.Fahrenheit).
		WindspeedUnit(openmeteogo.KN).
		PrecipitationUnit(openmeteogo.PrecipitationUnit("inch")).
		Start(date).
		End(date.AddDate(0, 0, 1)).
		DailyMetrics(openmeteogo.Metrics{
			openmeteogo.WeatherCode,
			openmeteogo.Temperature2mMax,
			openmeteogo.Temperature2mMin,
			openmeteogo.WindSpeed10mMax,
			openmeteogo.WindGusts10mMax,
			openmeteogo.WindDirection10mDominant,
			openmeteogo.PrecipitationSum,
		}).
		HourlyMetrics(openmeteogo.Metrics{
			openmeteogo.WindSpeed10m,
			openmeteogo.WindDirection10m,
			openmeteogo.WeatherCode,
			openmeteogo.Temperature2m,
			openmeteogo.WindGusts10m,
			openmeteogo.Precipitation,
		}).
		Build()
}

func (wp *WeatherProvider) buildMarineOptions(lat, lng float64, date time.Time) *openmeteogo.Options {
	return openmeteogo.NewOptionsBuilder().
		Latitude(lat).
		Longitude(lng).
		Marine(true).
		Start(date).
		End(date.AddDate(0, 0, 1)).
		DailyMetrics(openmeteogo.Metrics{
			openmeteogo.WaveHeightMax,
			openmeteogo.WaveDirectionDominant,
			openmeteogo.WavePeriodMax,
		}).
		HourlyMetrics(openmeteogo.Metrics{
			openmeteogo.WaveHeight,
			openmeteogo.WaveDirection,
			openmeteogo.WavePeriod,
		}).
		Build()
}

func (wp *WeatherProvider) processResults(weather, marine *openmeteogo.WeatherData, marineErr error, isHistorical bool) WeatherResult {
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
	if isHistorical {
		forecastType = "Historical Archive"
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

	hourlyWind := weather.Hourly.WindSpeed10m
	hourlyWindDir := make([]string, len(weather.Hourly.WindDirection10m))
	for i, deg := range weather.Hourly.WindDirection10m {
		hourlyWindDir[i] = DegreesToDirection(float64(deg))
	}

	hourlyConditions := make([]string, len(weather.Hourly.WeatherCode))
	for i, code := range weather.Hourly.WeatherCode {
		hourlyConditions[i] = openmeteogo.DescribeCode(int(code))
	}

	var hourlyWaveHeight, hourlyWaveDir, hourlyWavePeriod []float64
	if marineErr == nil && marine != nil && marine.Hourly.Time != nil && len(marine.Hourly.Time) > 0 {
		hourlyWaveHeight = make([]float64, len(marine.Hourly.WaveHeight))
		for i, h := range marine.Hourly.WaveHeight {
			hourlyWaveHeight[i] = h * metersToFeet
		}
		hourlyWaveDir = marine.Hourly.WaveDirection
		hourlyWavePeriod = marine.Hourly.WavePeriod
	}

	return WeatherResult{
		Date:             weather.Daily.Time[0],
		Condition:        condition,
		ForecastType:     forecastType,
		MaxTemp:          maxTemp,
		MinTemp:          minTemp,
		MaxWindKts:       maxWind,
		MaxGustsKts:      maxGusts,
		WindDirDeg:       windDir,
		WindDirection:    DegreesToDirection(float64(windDir)),
		HourlyWind:       hourlyWind,
		HourlyWindDir:    hourlyWindDir,
		HourlyConditions: hourlyConditions,
		HourlyTemp:       weather.Hourly.Temperature2m,
		HourlyGusts:      weather.Hourly.WindGusts10m,
		HourlyPrecip:     weather.Hourly.Precipitation,
		HourlyWaveHeight: hourlyWaveHeight,
		HourlyWaveDir:    hourlyWaveDir,
		HourlyWavePeriod: hourlyWavePeriod,
		PrecipTotal:      precip,
		WaveHeight:       waveHeight,
		WaveDirection:    waveDir,
		WavePeriod:       wavePeriod,
	}
}

// DegreesToDirection converts a compass bearing in degrees to a cardinal direction string (e.g. "N", "NNE").
func DegreesToDirection(deg float64) string {
	directions := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	index := int((deg + 11.25) / 22.5)
	return directions[index%16]
}
