package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"app/internal/model"

	"github.com/tpryan/openmeteogo"
)

const forecastWindowDays = 16
const metersToFeet = 3.28084

func degreesToCompass(deg float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	idx := int(math.Round(deg/22.5)) % 16
	return dirs[idx]
}

func fetchWeatherForDay(ctx context.Context, day model.VoyageDay) (model.WeatherSummary, error) {
	start := time.Now()
	date := day.TargetDate.Format("2006-01-02")
	daysOut := int(math.Round(time.Until(day.TargetDate).Hours() / 24))

	lat := day.EndLatitude
	lng := day.EndLongitude
	if lat == 0 && lng == 0 {
		lat = day.StartLatitude
		lng = day.StartLongitude
	}
	locName := day.PrimaryLocationName()

	slog.InfoContext(ctx, "[weather] Fetching weather for day",
		"day_id", day.ID,
		"location", locName,
		"target_date", date,
		"days_from_now", daysOut,
	)

	client := openmeteogo.NewClient()
	var weather *openmeteogo.WeatherData
	var source string

	if daysOut <= forecastWindowDays {
		slog.InfoContext(ctx, "[weather] Date within forecast window, using live forecast", "day_id", day.ID, "days_out", daysOut)
		opts := openmeteogo.NewOptionsBuilder().
			Latitude(lat).
			Longitude(lng).
			Start(day.TargetDate).
			End(day.TargetDate.AddDate(0, 0, 1)).
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
				openmeteogo.WindGusts10m,
				openmeteogo.Temperature2m,
				openmeteogo.WeatherCode,
				openmeteogo.Precipitation,
			}).
			TemperatureUnit(openmeteogo.Fahrenheit).
			WindspeedUnit(openmeteogo.KN).
			PrecipitationUnit(openmeteogo.PrecipitationUnit("inch")).
			Build()

		var err error
		weather, err = client.Get(opts)
		if err != nil {
			slog.WarnContext(ctx, "[weather] Forecast API failed, falling back to historical", "day_id", day.ID, "error", err)
		} else if len(weather.Daily.WeatherCode) == 0 {
			slog.WarnContext(ctx, "[weather] Forecast returned empty data, falling back to historical", "day_id", day.ID, "date", date)
			weather = nil
		} else {
			source = "forecast"
		}
	}

	if weather == nil {
		historicalDate := day.TargetDate.AddDate(-1, 0, 0)
		slog.InfoContext(ctx, "[weather] Fetching historical data", "day_id", day.ID, "historical_date", historicalDate.Format("2006-01-02"))

		opts := openmeteogo.NewOptionsBuilder().
			Latitude(lat).
			Longitude(lng).
			Start(historicalDate).
			End(historicalDate.AddDate(0, 0, 1)).
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
				openmeteogo.WindGusts10m,
				openmeteogo.Temperature2m,
				openmeteogo.WeatherCode,
				openmeteogo.Precipitation,
			}).
			TemperatureUnit(openmeteogo.Fahrenheit).
			WindspeedUnit(openmeteogo.KN).
			PrecipitationUnit(openmeteogo.PrecipitationUnit("inch")).
			Build()

		var err error
		weather, err = client.Get(opts)
		if err != nil {
			return model.WeatherSummary{}, fmt.Errorf("historical fetch for day %d (%s): %w", day.ID, date, err)
		}
		if len(weather.Daily.WeatherCode) == 0 {
			return model.WeatherSummary{}, fmt.Errorf("historical data empty for day %d on %s", day.ID, historicalDate.Format("2006-01-02"))
		}
		source = "historical"
		slog.InfoContext(ctx, "[weather] Using historical data as fallback", "day_id", day.ID, "historical_date", historicalDate.Format("2006-01-02"))
	}

	condition := openmeteogo.DescribeCode(int(weather.Daily.WeatherCode[0]))
	summary := condition
	if source == "historical" {
		summary = "(Historical avg) " + summary
	}

	hourlyConditions := make([]string, len(weather.Hourly.WeatherCode))
	for i, code := range weather.Hourly.WeatherCode {
		hourlyConditions[i] = openmeteogo.DescribeCode(int(code))
	}

	ws := model.WeatherSummary{
		Summary:          summary,
		Condition:        condition,
		HourlyWind:       weather.Hourly.WindSpeed10m,
		HourlyWindDir:    make([]string, len(weather.Hourly.WindDirection10m)),
		HourlyConditions: hourlyConditions,
		HourlyTemp:       weather.Hourly.Temperature2m,
		HourlyGusts:      weather.Hourly.WindGusts10m,
		HourlyPrecip:     weather.Hourly.Precipitation,
	}
	if len(weather.Daily.Temperature2mMin) > 0 {
		ws.TempMinF = weather.Daily.Temperature2mMin[0]
	}
	if len(weather.Daily.Temperature2mMax) > 0 {
		ws.TempMaxF = weather.Daily.Temperature2mMax[0]
	}
	if len(weather.Daily.WindSpeed10mMax) > 0 {
		ws.WindSpeedKt = weather.Daily.WindSpeed10mMax[0]
	}
	if len(weather.Daily.WindDirection10mDominant) > 0 {
		ws.WindDirection = degreesToCompass(float64(weather.Daily.WindDirection10mDominant[0]))
	}
	for i, deg := range weather.Hourly.WindDirection10m {
		ws.HourlyWindDir[i] = degreesToCompass(float64(deg))
	}

	marine, marineErr := fetchMarineWaves(ctx, lat, lng, day.TargetDate)
	switch {
	case marineErr != nil:
		slog.WarnContext(ctx, "[weather] Marine API unreachable, skipping wave data", "day_id", day.ID, "error", marineErr)
	case marine == nil:
		// The request succeeded but every value came back null, so this
		// position sits outside the wave model's domain. Flag it instead of
		// leaving zeroed fields that read as a flat calm.
		ws.WaveDataUnavailable = true
		slog.InfoContext(ctx, "[weather] No wave model coverage at position, skipping wave data",
			"day_id", day.ID, "lat", lat, "lng", lng)
	default:
		if v, ok := firstValue(marine.Daily.WaveHeightMax, metersToFeet); ok {
			ws.WaveHeightFt = v
		}
		if v, ok := firstValue(marine.Daily.WaveDirectionDominant, 1); ok {
			ws.WaveDirection = v
		}
		if v, ok := firstValue(marine.Daily.WavePeriodMax, 1); ok {
			ws.WavePeriod = v
		}
		ws.HourlyWaveHeight = toFloats(marine.Hourly.WaveHeight, metersToFeet)
		ws.HourlyWavePeriod = toFloats(marine.Hourly.WavePeriod, 1)
		ws.HourlyWaveDir = toFloats(marine.Hourly.WaveDirection, 1)
		slog.DebugContext(ctx, "[weather] Wave data fetched", "day_id", day.ID, "wave_height_ft", ws.WaveHeightFt)
	}

	ws.DebugDurationMs = time.Since(start).Milliseconds()
	slog.InfoContext(ctx, "[weather] Day weather complete", "day_id", day.ID, "source", source, "duration_ms", ws.DebugDurationMs)
	return ws, nil
}

func (h *Handler) UpdateVoyageWeather(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := voyage.ID

	ctx := r.Context()
	days, err := h.DB.ListDays(ctx, voyageID, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list days")
		return
	}

	slog.InfoContext(ctx, "[weather] UpdateVoyageWeather started", "voyage_id", voyageID, "day_count", len(days))

	updated, failed := 0, 0
	for _, day := range days {
		weather, err := fetchWeatherForDay(ctx, day)
		if err != nil {
			slog.ErrorContext(ctx, "[weather] Failed to fetch weather", "day_id", day.ID, "error", err)
			failed++
			continue
		}
		if err := h.DB.UpsertWeatherBriefing(ctx, day.ID, weather); err != nil {
			slog.ErrorContext(ctx, "[weather] Failed to save weather", "day_id", day.ID, "error", err)
			failed++
			continue
		}
		updated++
	}

	slog.InfoContext(ctx, "[weather] UpdateVoyageWeather complete", "voyage_id", voyageID, "updated", updated, "failed", failed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total":   len(days),
		"updated": updated,
		"failed":  failed,
	})
}

func (h *Handler) UpdateAllFutureWeather(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slog.InfoContext(ctx, "[weather] UpdateAllFutureWeather started")

	days, err := h.DB.ListAllFutureDays(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "[weather] Failed to list future days", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	slog.InfoContext(ctx, "[weather] Future days found", "count", len(days))

	updated := 0
	failed := 0
	for _, day := range days {
		weather, err := fetchWeatherForDay(ctx, day)
		if err != nil {
			slog.ErrorContext(ctx, "[weather] Failed to fetch weather for day", "day_id", day.ID, "location", day.PrimaryLocationName(), "error", err)
			failed++
			continue
		}
		if err := h.DB.UpsertWeatherBriefing(ctx, day.ID, weather); err != nil {
			slog.ErrorContext(ctx, "[weather] Failed to save weather briefing", "day_id", day.ID, "error", err)
			failed++
			continue
		}
		updated++
	}

	slog.InfoContext(ctx, "[weather] UpdateAllFutureWeather complete", "total", len(days), "updated", updated, "failed", failed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total":   len(days),
		"updated": updated,
		"failed":  failed,
	})
}
