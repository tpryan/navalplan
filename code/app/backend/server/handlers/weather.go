package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	appcontext "app/context"
	"app/models"

	"github.com/tpryan/openmeteogo"
)

// forecastWindowDays is the maximum number of days ahead Open-Meteo forecast covers.
const forecastWindowDays = 16

const metersToFeet = 3.28084

func degreesToCompass(deg float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	idx := int(math.Round(deg/22.5)) % 16
	return dirs[idx]
}

func fetchWeatherForStop(ctx context.Context, stop models.Stop) (models.WeatherSummary, error) {
	start := time.Now()
	date := stop.TargetDate.Format("2006-01-02")
	daysOut := int(math.Round(time.Until(stop.TargetDate).Hours() / 24))

	slog.InfoContext(ctx, "[weather] Fetching weather for stop",
		"stop_id", stop.ID,
		"location", stop.LocationName,
		"target_date", date,
		"days_from_now", daysOut,
	)

	client := openmeteogo.NewClient()
	var weather *openmeteogo.WeatherData
	var source string

	if daysOut <= forecastWindowDays {
		slog.InfoContext(ctx, "[weather] Date within forecast window, using live forecast", "stop_id", stop.ID, "days_out", daysOut)
		opts := openmeteogo.NewOptionsBuilder().
			Latitude(stop.Latitude).
			Longitude(stop.Longitude).
			Start(stop.TargetDate).
			End(stop.TargetDate).
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
			slog.WarnContext(ctx, "[weather] Forecast API failed, falling back to historical", "stop_id", stop.ID, "error", err)
		} else if len(weather.Daily.WeatherCode) == 0 {
			slog.WarnContext(ctx, "[weather] Forecast returned empty data, falling back to historical", "stop_id", stop.ID, "date", date)
			weather = nil
		} else {
			source = "forecast"
		}
	}

	if weather == nil {
		// Use same calendar date from prior year as historical baseline.
		historicalDate := stop.TargetDate.AddDate(-1, 0, 0)
		slog.InfoContext(ctx, "[weather] Fetching historical data", "stop_id", stop.ID, "historical_date", historicalDate.Format("2006-01-02"))

		opts := openmeteogo.NewOptionsBuilder().
			Latitude(stop.Latitude).
			Longitude(stop.Longitude).
			Start(historicalDate).
			End(historicalDate).
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
			return models.WeatherSummary{}, fmt.Errorf("historical fetch for stop %d (%s): %w", stop.ID, date, err)
		}
		if len(weather.Daily.WeatherCode) == 0 {
			return models.WeatherSummary{}, fmt.Errorf("historical data empty for stop %d on %s", stop.ID, historicalDate.Format("2006-01-02"))
		}
		source = "historical"
		slog.InfoContext(ctx, "[weather] Using historical data as fallback", "stop_id", stop.ID, "historical_date", historicalDate.Format("2006-01-02"))
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

	ws := models.WeatherSummary{
		Summary:          summary,
		Condition:        condition,
		TempMinF:         weather.Daily.Temperature2mMin[0],
		TempMaxF:         weather.Daily.Temperature2mMax[0],
		WindSpeedKt:      weather.Daily.WindSpeed10mMax[0],
		WindDirection:    degreesToCompass(float64(weather.Daily.WindDirection10mDominant[0])),
		HourlyWind:       weather.Hourly.WindSpeed10m,
		HourlyWindDir:    make([]string, len(weather.Hourly.WindDirection10m)),
		HourlyConditions: hourlyConditions,
		HourlyTemp:       weather.Hourly.Temperature2m,
		HourlyGusts:      weather.Hourly.WindGusts10m,
		HourlyPrecip:     weather.Hourly.Precipitation,
	}

	for i, deg := range weather.Hourly.WindDirection10m {
		ws.HourlyWindDir[i] = degreesToCompass(float64(deg))
	}

	// Marine wave data — best-effort, does not fail the whole stop.
	marineOpts := openmeteogo.NewOptionsBuilder().
		Marine(true).
		Latitude(stop.Latitude).
		Longitude(stop.Longitude).
		Start(stop.TargetDate).
		End(stop.TargetDate).
		DailyMetrics(openmeteogo.Metrics{
			openmeteogo.WaveHeightMax,
		}).
		HourlyMetrics(openmeteogo.Metrics{
			openmeteogo.WaveHeight,
			openmeteogo.WavePeriod,
			openmeteogo.WaveDirection,
		}).
		Build()

	marine, err := client.Get(marineOpts)
	if err != nil {
		slog.WarnContext(ctx, "[weather] Marine API unreachable, skipping wave data", "stop_id", stop.ID, "error", err)
	} else if len(marine.Daily.WaveHeightMax) > 0 {
		ws.WaveHeightFt = marine.Daily.WaveHeightMax[0] * metersToFeet
		
		if len(marine.Hourly.WaveHeight) > 0 {
			ws.HourlyWaveHeight = make([]float64, len(marine.Hourly.WaveHeight))
			for i, h := range marine.Hourly.WaveHeight {
				ws.HourlyWaveHeight[i] = h * metersToFeet
			}
		}
		ws.HourlyWavePeriod = marine.Hourly.WavePeriod
		ws.HourlyWaveDir = marine.Hourly.WaveDirection
		slog.DebugContext(ctx, "[weather] Wave data fetched", "stop_id", stop.ID, "wave_height_ft", ws.WaveHeightFt)
	}

	ws.DebugDurationMs = time.Since(start).Milliseconds()
	slog.InfoContext(ctx, "[weather] Stop weather complete", "stop_id", stop.ID, "source", source, "duration_ms", ws.DebugDurationMs)
	return ws, nil
}

func (h *Handler) UpdateVoyageWeather(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	ctx := r.Context()
	stops, err := h.DB.ListStops(ctx, voyageID, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list stops")
		return
	}

	slog.InfoContext(ctx, "[weather] UpdateVoyageWeather started", "voyage_id", voyageID, "stop_count", len(stops))

	updated, failed := 0, 0
	for _, stop := range stops {
		weather, err := fetchWeatherForStop(ctx, stop)
		if err != nil {
			slog.ErrorContext(ctx, "[weather] Failed to fetch weather", "stop_id", stop.ID, "error", err)
			failed++
			continue
		}
		if err := h.DB.UpsertWeatherBriefing(ctx, stop.ID, weather); err != nil {
			slog.ErrorContext(ctx, "[weather] Failed to save weather", "stop_id", stop.ID, "error", err)
			failed++
			continue
		}
		updated++
	}

	slog.InfoContext(ctx, "[weather] UpdateVoyageWeather complete", "voyage_id", voyageID, "updated", updated, "failed", failed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total":   len(stops),
		"updated": updated,
		"failed":  failed,
	})
}

func (h *Handler) UpdateAllFutureWeather(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slog.InfoContext(ctx, "[weather] UpdateAllFutureWeather started")

	stops, err := h.DB.ListAllFutureStops(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "[weather] Failed to list future stops", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	slog.InfoContext(ctx, "[weather] Future stops found", "count", len(stops))

	updated := 0
	failed := 0
	for _, stop := range stops {
		weather, err := fetchWeatherForStop(ctx, stop)
		if err != nil {
			slog.ErrorContext(ctx, "[weather] Failed to fetch weather for stop", "stop_id", stop.ID, "location", stop.LocationName, "error", err)
			failed++
			continue
		}
		if err := h.DB.UpsertWeatherBriefing(ctx, stop.ID, weather); err != nil {
			slog.ErrorContext(ctx, "[weather] Failed to save weather briefing", "stop_id", stop.ID, "error", err)
			failed++
			continue
		}
		updated++
	}

	slog.InfoContext(ctx, "[weather] UpdateAllFutureWeather complete", "total", len(stops), "updated", updated, "failed", failed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total":   len(stops),
		"updated": updated,
		"failed":  failed,
	})
}
