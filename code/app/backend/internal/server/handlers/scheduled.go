package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

func (h *Handler) ScheduledMaintenance(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	slog.InfoContext(ctx, "[scheduled] Starting hourly maintenance")

	stops, err := h.DB.ListStopsInWindow(ctx, 2)
	if err != nil {
		slog.ErrorContext(ctx, "[scheduled] Failed to list stops in window", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	if len(stops) == 0 {
		slog.InfoContext(ctx, "[scheduled] No stops found in the 2-day window")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "no stops to process"}`))
		return
	}

	slog.InfoContext(ctx, "[scheduled] Processing stops", "count", len(stops))

	processedWeather := 0
	processedLookout := 0

	for _, stop := range stops {
		weather, err := fetchWeatherForStop(ctx, stop)
		if err != nil {
			slog.ErrorContext(ctx, "[scheduled] Failed to fetch weather", "stop_id", stop.ID, "error", err)
		} else {
			if err := h.DB.UpsertWeatherBriefing(ctx, stop.ID, weather); err != nil {
				slog.ErrorContext(ctx, "[scheduled] Failed to save weather", "stop_id", stop.ID, "error", err)
			} else {
				processedWeather++
			}
		}

		briefing, err := h.DB.GetBriefing(ctx, stop.ID)
		if err != nil {
			slog.WarnContext(ctx, "[scheduled] Failed to fetch briefing for stop", "stop_id", stop.ID, "error", err)
			continue
		}
		if briefing == nil {
			continue
		}

		allVoyageStops, err := h.DB.ListStops(ctx, stop.VoyageID, 0, 0)
		if err != nil {
			slog.ErrorContext(ctx, "[scheduled] Failed to list voyage stops", "stop_id", stop.ID, "voyage_id", stop.VoyageID, "error", err)
			continue
		}
		sort.Slice(allVoyageStops, func(i, j int) bool { return allVoyageStops[i].TargetDate.Before(allVoyageStops[j].TargetDate) })

		sessionID := fmt.Sprintf("scheduled_lookout_%d_%d", stop.ID, time.Now().Unix())

		func() {
			h.ResearchSem <- struct{}{}
			defer func() { <-h.ResearchSem }()
			h.performLookoutAuditLogic(&stop, briefing, allVoyageStops, sessionID)
		}()

		processedLookout++
	}

	slog.InfoContext(ctx, "[scheduled] Maintenance complete",
		"weather_updated", processedWeather,
		"lookout_audits", processedLookout,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message":         "maintenance complete",
		"weather_updated": processedWeather,
		"lookout_audits":  processedLookout,
	})
}
