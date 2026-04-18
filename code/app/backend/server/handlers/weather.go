package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"app/models"
)

type openMeteoForecastResponse struct {
	Daily struct {
		Time                    []string  `json:"time"`
		WeatherCode             []int     `json:"weathercode"`
		Temperature2mMax        []float64 `json:"temperature_2m_max"`
		Temperature2mMin        []float64 `json:"temperature_2m_min"`
		WindSpeed10mMax         []float64 `json:"windspeed_10m_max"`
		WindDirection10mDom     []float64 `json:"winddirection_10m_dominant"`
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

func fetchWeatherForStop(stop models.Stop) (models.WeatherSummary, error) {
	start := time.Now()
	date := stop.TargetDate.Format("2006-01-02")

	forecastURL := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&daily=weathercode,temperature_2m_max,temperature_2m_min,windspeed_10m_max,winddirection_10m_dominant&wind_speed_unit=kn&temperature_unit=fahrenheit&timezone=auto&start_date=%s&end_date=%s",
		stop.Latitude, stop.Longitude, date, date,
	)

	forecastResp, err := http.Get(forecastURL) //nolint:noctx
	if err != nil {
		return models.WeatherSummary{}, fmt.Errorf("forecast fetch: %w", err)
	}
	defer forecastResp.Body.Close()

	body, err := io.ReadAll(forecastResp.Body)
	if err != nil {
		return models.WeatherSummary{}, fmt.Errorf("forecast read: %w", err)
	}

	var forecast openMeteoForecastResponse
	if err := json.Unmarshal(body, &forecast); err != nil {
		return models.WeatherSummary{}, fmt.Errorf("forecast parse: %w", err)
	}

	if len(forecast.Daily.WeatherCode) == 0 {
		return models.WeatherSummary{}, fmt.Errorf("no forecast data for %s", date)
	}

	condition, summary := wmoCondition(forecast.Daily.WeatherCode[0])
	ws := models.WeatherSummary{
		Summary:       summary,
		Condition:     condition,
		TempMinF:      forecast.Daily.Temperature2mMin[0],
		TempMaxF:      forecast.Daily.Temperature2mMax[0],
		WindSpeedKt:   forecast.Daily.WindSpeed10mMax[0],
		WindDirection: degreesToCompass(forecast.Daily.WindDirection10mDom[0]),
	}

	marineURL := fmt.Sprintf(
		"https://marine-api.open-meteo.com/v1/marine?latitude=%f&longitude=%f&daily=wave_height_max&length_unit=imperial&timezone=auto&start_date=%s&end_date=%s",
		stop.Latitude, stop.Longitude, date, date,
	)

	marineResp, err := http.Get(marineURL) //nolint:noctx
	if err == nil {
		defer marineResp.Body.Close()
		if marineBody, err := io.ReadAll(marineResp.Body); err == nil {
			var marine openMeteoMarineResponse
			if json.Unmarshal(marineBody, &marine) == nil && len(marine.Daily.WaveHeightMax) > 0 {
				ws.WaveHeightFt = marine.Daily.WaveHeightMax[0]
			}
		}
	}

	ws.DebugDurationMs = time.Since(start).Milliseconds()
	return ws, nil
}

func (h *Handler) UpdateAllFutureWeather(w http.ResponseWriter, r *http.Request) {
	stops, err := h.DB.ListAllFutureStops(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list future stops", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	updated := 0
	failed := 0
	for _, stop := range stops {
		weather, err := fetchWeatherForStop(stop)
		if err != nil {
			slog.WarnContext(r.Context(), "Failed to fetch weather for stop", "stop_id", stop.ID, "error", err)
			failed++
			continue
		}
		if err := h.DB.UpsertWeatherBriefing(r.Context(), stop.ID, weather); err != nil {
			slog.ErrorContext(r.Context(), "Failed to upsert weather briefing", "stop_id", stop.ID, "error", err)
			failed++
			continue
		}
		updated++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total":   len(stops),
		"updated": updated,
		"failed":  failed,
	})
}
