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

	days, err := h.DB.ListDaysInWindow(ctx, 2)
	if err != nil {
		slog.ErrorContext(ctx, "[scheduled] Failed to list days in window", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	if len(days) == 0 {
		slog.InfoContext(ctx, "[scheduled] No days found in the 2-day window")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "no days to process"}`))
		return
	}

	slog.InfoContext(ctx, "[scheduled] Processing days", "count", len(days))

	processedWeather := 0
	processedLookout := 0

	for _, day := range days {
		weather, err := fetchWeatherForDay(ctx, day)
		if err != nil {
			slog.ErrorContext(ctx, "[scheduled] Failed to fetch weather", "day_id", day.ID, "error", err)
		} else {
			if err := h.DB.UpsertWeatherBriefing(ctx, day.ID, weather); err != nil {
				slog.ErrorContext(ctx, "[scheduled] Failed to save weather", "day_id", day.ID, "error", err)
			} else {
				processedWeather++
			}
		}

		briefing, err := h.DB.GetBriefing(ctx, day.ID)
		if err != nil {
			slog.WarnContext(ctx, "[scheduled] Failed to fetch briefing for day", "day_id", day.ID, "error", err)
			continue
		}
		if briefing == nil {
			continue
		}

		allVoyageDays, err := h.DB.ListDays(ctx, day.VoyageID, 0, 0)
		if err != nil {
			slog.ErrorContext(ctx, "[scheduled] Failed to list voyage days", "day_id", day.ID, "voyage_id", day.VoyageID, "error", err)
			continue
		}
		sort.Slice(allVoyageDays, func(i, j int) bool { return allVoyageDays[i].TargetDate.Before(allVoyageDays[j].TargetDate) })

		sessionID := fmt.Sprintf("scheduled_lookout_%d_%d", day.ID, time.Now().Unix())

		func() {
			h.ResearchSem <- struct{}{}
			defer func() { <-h.ResearchSem }()
			h.performLookoutAuditLogic(&day, briefing, allVoyageDays, sessionID)
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
