package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"app/models"
)

// ScheduledMaintenance handles hourly background updates for active voyages.
// It updates weather and re-runs safety audits for stops in the next 2 days.
func (h *Handler) ScheduledMaintenance(w http.ResponseWriter, r *http.Request) {
	// This endpoint should ideally be protected by a shared secret or internal-only access.
	// For now, we assume it's triggered by a trusted scheduler.

	ctx := context.Background()
	slog.InfoContext(ctx, "[scheduled] Starting hourly maintenance")

	// 1. Find stops in the 2-day window
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

	// Group stops by voyage for lookout logic
	voyageStops := make(map[int64][]models.Stop)
	for _, s := range stops {
		voyageStops[s.VoyageID] = append(voyageStops[s.VoyageID], s)
	}

	processedWeather := 0
	processedLookout := 0

	for _, stop := range stops {
		// 2. Update Weather
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

		// 3. Run Lookout Audit
		briefing, err := h.DB.GetBriefing(ctx, stop.ID)
		if err != nil || briefing == nil {
			continue
		}

		// We need all stops for this voyage to calculate distances
		allVoyageStops, err := h.DB.ListStops(ctx, stop.VoyageID, 0, 0)
		if err != nil {
			slog.ErrorContext(ctx, "[scheduled] Failed to list voyage stops", "voyage_id", stop.ID, "error", err)
			continue
		}
		sort.Slice(allVoyageStops, func(i, j int) bool { return allVoyageStops[i].TargetDate.Before(allVoyageStops[j].TargetDate) })

		sessionID := fmt.Sprintf("scheduled_lookout_%d_%d", stop.ID, time.Now().Unix())

		// Use a semaphore to limit concurrent agent calls
		h.ResearchSem <- struct{}{}
		h.performLookoutAuditLogic(&stop, briefing, allVoyageStops, sessionID)
		<-h.ResearchSem

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
