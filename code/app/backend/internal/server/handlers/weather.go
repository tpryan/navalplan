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

	"github.com/tpryan/openmeteogo/v2"
)

const forecastWindowDays = 16
const metersToFeet = 3.28084

// scaleFloats multiplies every entry in place and returns the slice. Used to
// convert the metres the marine API reports into the feet stored on
// WeatherSummary.
func scaleFloats(vals []float64, scale float64) []float64 {
	for i := range vals {
		vals[i] *= scale
	}
	return vals
}

// fetchWavePoint retrieves the sea state at a single position. It returns nil
// when the request itself failed, which is unknown rather than absent, and a
// WavePoint with Unavailable set when the API answered but the position lies
// outside the wave model's domain.
func fetchWavePoint(ctx context.Context, client *openmeteogo.Client, lat, lng float64, date time.Time, name string, dayID int64) *model.WavePoint {
	opts := openmeteogo.NewOptionsBuilder().
		Marine(true).
		Latitude(lat).
		Longitude(lng).
		Start(date).
		End(date.AddDate(0, 0, 1)).
		DailyMetrics(openmeteogo.Metrics{
			openmeteogo.WaveHeightMax,
			openmeteogo.WaveDirectionDominant,
			openmeteogo.WavePeriodMax,
		}).
		HourlyMetrics(openmeteogo.Metrics{
			openmeteogo.WaveHeight,
			openmeteogo.WavePeriod,
			openmeteogo.WaveDirection,
		}).
		Build()

	marine, err := client.Get(opts)
	if err != nil {
		slog.WarnContext(ctx, "[weather] Marine API unreachable, skipping wave data",
			"day_id", dayID, "location", name, "error", err)
		return nil
	}

	point := &model.WavePoint{LocationName: name}
	if marine == nil || (!openmeteogo.HasAny(marine.Daily.WaveHeightMax) && !openmeteogo.HasAny(marine.Hourly.WaveHeight)) {
		// Answered, but null throughout: outside the model's water mask.
		point.Unavailable = true
		slog.InfoContext(ctx, "[weather] No wave model coverage at position",
			"day_id", dayID, "location", name, "lat", lat, "lng", lng)
		return point
	}

	if v, ok := openmeteogo.Value(marine.Daily.WaveHeightMax); ok {
		point.HeightFt = v * metersToFeet
	}
	if v, ok := openmeteogo.Value(marine.Daily.WaveDirectionDominant); ok {
		point.Direction = v
	}
	if v, ok := openmeteogo.Value(marine.Daily.WavePeriodMax); ok {
		point.Period = v
	}
	point.HourlyHeight = scaleFloats(openmeteogo.Floats(marine.Hourly.WaveHeight), metersToFeet)
	point.HourlyPeriod = openmeteogo.Floats(marine.Hourly.WavePeriod)
	point.HourlyDir = openmeteogo.Floats(marine.Hourly.WaveDirection)
	return point
}

// applyRepresentativeWaves collapses the per-endpoint wave data into the flat
// summary fields, preferring the endpoint with the heavier sea so a single
// number stays conservative for a passage. WaveDataUnavailable is set only
// when no endpoint had coverage, so a day that departs into real swell still
// reports it even when the destination lies outside the model.
func applyRepresentativeWaves(ws *model.WeatherSummary) {
	representative := ws.StartWaves
	if ws.EndWaves != nil && !ws.EndWaves.Unavailable {
		if representative == nil || representative.Unavailable || ws.EndWaves.HeightFt > representative.HeightFt {
			representative = ws.EndWaves
		}
	}
	if representative != nil && !representative.Unavailable {
		ws.WaveHeightFt = representative.HeightFt
		ws.WaveDirection = representative.Direction
		ws.WavePeriod = representative.Period
		ws.HourlyWaveHeight = representative.HourlyHeight
		ws.HourlyWavePeriod = representative.HourlyPeriod
		ws.HourlyWaveDir = representative.HourlyDir
	}

	startMissing := ws.StartWaves == nil || ws.StartWaves.Unavailable
	endMissing := ws.EndWaves == nil || ws.EndWaves.Unavailable
	ws.WaveDataUnavailable = startMissing && endMissing
}

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

	// A day runs between two positions, and the sea state at each can differ
	// sharply: a passage may leave an exposed coast for an estuary the wave
	// model does not cover at all. Fetch both ends rather than the
	// destination alone.
	ws.StartWaves = fetchWavePoint(ctx, client, day.StartLatitude, day.StartLongitude, day.TargetDate, day.StartLocationName, day.ID)
	if lat != day.StartLatitude || lng != day.StartLongitude {
		ws.EndWaves = fetchWavePoint(ctx, client, lat, lng, day.TargetDate, day.EndLocationName, day.ID)
	}

	applyRepresentativeWaves(&ws)

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
