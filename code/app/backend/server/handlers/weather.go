package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	appcontext "app/context"
	"app/models"
)

// forecastWindowDays is the maximum number of days ahead Open-Meteo forecast covers.
const forecastWindowDays = 16

type openMeteoForecastResponse struct {
	Daily struct {
		Time                []string  `json:"time"`
		WeatherCode         []int     `json:"weathercode"`
		Temperature2mMax    []float64 `json:"temperature_2m_max"`
		Temperature2mMin    []float64 `json:"temperature_2m_min"`
		WindSpeed10mMax     []float64 `json:"windspeed_10m_max"`
		WindDirection10mDom []float64 `json:"winddirection_10m_dominant"`
	} `json:"daily"`
}

type openMeteoMarineResponse struct {
	Daily struct {
		Time          []string  `json:"time"`
		WaveHeightMax []float64 `json:"wave_height_max"`
	} `json:"daily"`
}

func wmoCondition(code int) (condition, summary string) {
	switch {
	case code == 0:
		return "Clear", "Clear skies"
	case code <= 3:
		return "Partly Cloudy", "Partly cloudy conditions"
	case code <= 48:
		return "Foggy", "Fog or depositing rime fog"
	case code <= 55:
		return "Drizzle", "Light drizzle"
	case code <= 57:
		return "Freezing Drizzle", "Freezing drizzle"
	case code <= 65:
		return "Rain", "Rain"
	case code <= 67:
		return "Freezing Rain", "Freezing rain"
	case code <= 75:
		return "Snow", "Snowfall"
	case code == 77:
		return "Snow Grains", "Snow grains"
	case code <= 82:
		return "Showers", "Rain showers"
	case code <= 86:
		return "Snow Showers", "Snow showers"
	case code == 95:
		return "Thunderstorm", "Thunderstorm"
	case code <= 99:
		return "Thunderstorm", "Thunderstorm with hail"
	default:
		return "Unknown", "Unknown conditions"
	}
}

func degreesToCompass(deg float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	idx := int(math.Round(deg/22.5)) % 16
	return dirs[idx]
}

func fetchOpenMeteoForecast(ctx context.Context, lat, lng float64, date string) (*openMeteoForecastResponse, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&daily=weathercode,temperature_2m_max,temperature_2m_min,windspeed_10m_max,winddirection_10m_dominant&wind_speed_unit=kn&temperature_unit=fahrenheit&timezone=auto&start_date=%s&end_date=%s",
		lat, lng, date, date,
	)
	slog.DebugContext(ctx, "[weather] Calling forecast API", "url", url)

	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("forecast HTTP get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("forecast read body: %w", err)
	}

	slog.DebugContext(ctx, "[weather] Forecast API response", "status", resp.StatusCode, "bytes", len(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("forecast API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result openMeteoForecastResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("forecast parse: %w", err)
	}
	return &result, nil
}

func fetchOpenMeteoHistorical(ctx context.Context, lat, lng float64, date string) (*openMeteoForecastResponse, error) {
	url := fmt.Sprintf(
		"https://archive-api.open-meteo.com/v1/archive?latitude=%f&longitude=%f&daily=weathercode,temperature_2m_max,temperature_2m_min,windspeed_10m_max,winddirection_10m_dominant&wind_speed_unit=kn&temperature_unit=fahrenheit&timezone=auto&start_date=%s&end_date=%s",
		lat, lng, date, date,
	)
	slog.DebugContext(ctx, "[weather] Calling historical archive API", "url", url)

	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("historical HTTP get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("historical read body: %w", err)
	}

	slog.DebugContext(ctx, "[weather] Historical API response", "status", resp.StatusCode, "bytes", len(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("historical API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result openMeteoForecastResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("historical parse: %w", err)
	}
	return &result, nil
}

func fetchWeatherForStop(ctx context.Context, stop models.Stop) (models.WeatherSummary, error) {
	start := time.Now()
	date := stop.TargetDate.Format("2006-01-02")
	daysOut := int(time.Until(stop.TargetDate).Hours() / 24)

	slog.InfoContext(ctx, "[weather] Fetching weather for stop",
		"stop_id", stop.ID,
		"location", stop.LocationName,
		"target_date", date,
		"days_from_now", daysOut,
	)

	var forecast *openMeteoForecastResponse
	var source string

	if daysOut <= forecastWindowDays {
		slog.InfoContext(ctx, "[weather] Date within forecast window, using live forecast", "stop_id", stop.ID, "days_out", daysOut)
		f, err := fetchOpenMeteoForecast(ctx, stop.Latitude, stop.Longitude, date)
		if err != nil {
			slog.WarnContext(ctx, "[weather] Forecast API failed, falling back to historical", "stop_id", stop.ID, "error", err)
		} else if len(f.Daily.WeatherCode) == 0 {
			slog.WarnContext(ctx, "[weather] Forecast returned empty data, falling back to historical", "stop_id", stop.ID, "date", date)
		} else {
			forecast = f
			source = "forecast"
		}
	} else {
		slog.InfoContext(ctx, "[weather] Date beyond forecast window, using historical average", "stop_id", stop.ID, "days_out", daysOut, "window", forecastWindowDays)
	}

	if forecast == nil {
		// Use same calendar date from prior year as historical baseline.
		historicalDate := stop.TargetDate.AddDate(-1, 0, 0).Format("2006-01-02")
		slog.InfoContext(ctx, "[weather] Fetching historical data", "stop_id", stop.ID, "historical_date", historicalDate)

		f, err := fetchOpenMeteoHistorical(ctx, stop.Latitude, stop.Longitude, historicalDate)
		if err != nil {
			return models.WeatherSummary{}, fmt.Errorf("historical fetch for stop %d (%s): %w", stop.ID, date, err)
		}
		if len(f.Daily.WeatherCode) == 0 {
			return models.WeatherSummary{}, fmt.Errorf("historical data empty for stop %d on %s", stop.ID, historicalDate)
		}
		forecast = f
		source = "historical"
		slog.InfoContext(ctx, "[weather] Using historical data as fallback", "stop_id", stop.ID, "historical_date", historicalDate)
	}

	condition, summary := wmoCondition(forecast.Daily.WeatherCode[0])
	if source == "historical" {
		summary = "(Historical avg) " + summary
	}

	ws := models.WeatherSummary{
		Summary:       summary,
		Condition:     condition,
		TempMinF:      forecast.Daily.Temperature2mMin[0],
		TempMaxF:      forecast.Daily.Temperature2mMax[0],
		WindSpeedKt:   forecast.Daily.WindSpeed10mMax[0],
		WindDirection: degreesToCompass(forecast.Daily.WindDirection10mDom[0]),
	}

	slog.InfoContext(ctx, "[weather] Forecast data parsed",
		"stop_id", stop.ID,
		"source", source,
		"condition", ws.Condition,
		"temp_max_f", ws.TempMaxF,
		"temp_min_f", ws.TempMinF,
		"wind_speed_kt", ws.WindSpeedKt,
		"wind_direction", ws.WindDirection,
	)

	// Marine wave data — best-effort, does not fail the whole stop.
	marineURL := fmt.Sprintf(
		"https://marine-api.open-meteo.com/v1/marine?latitude=%f&longitude=%f&daily=wave_height_max&length_unit=imperial&timezone=auto&start_date=%s&end_date=%s",
		stop.Latitude, stop.Longitude, date, date,
	)
	slog.DebugContext(ctx, "[weather] Calling marine API", "stop_id", stop.ID, "url", marineURL)

	marineResp, err := http.Get(marineURL) //nolint:noctx
	if err != nil {
		slog.WarnContext(ctx, "[weather] Marine API unreachable, skipping wave height", "stop_id", stop.ID, "error", err)
	} else {
		defer marineResp.Body.Close()
		if marineBody, err := io.ReadAll(marineResp.Body); err != nil {
			slog.WarnContext(ctx, "[weather] Marine API body read failed", "stop_id", stop.ID, "error", err)
		} else {
			var marine openMeteoMarineResponse
			if err := json.Unmarshal(marineBody, &marine); err != nil {
				slog.WarnContext(ctx, "[weather] Marine API parse failed", "stop_id", stop.ID, "error", err)
			} else if len(marine.Daily.WaveHeightMax) > 0 {
				ws.WaveHeightFt = marine.Daily.WaveHeightMax[0]
				slog.DebugContext(ctx, "[weather] Wave height fetched", "stop_id", stop.ID, "wave_height_ft", ws.WaveHeightFt)
			} else {
				slog.DebugContext(ctx, "[weather] Marine API returned no wave data (likely inland stop)", "stop_id", stop.ID)
			}
		}
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
