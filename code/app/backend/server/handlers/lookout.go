package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appcontext "app/context"
	"app/models"
)

func (h *Handler) TriggerLookoutAudit(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Stop ID")
		return
	}

	stop, err := h.DB.GetStop(r.Context(), stopID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Stop not found")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), stop.VoyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	briefing, err := h.DB.GetBriefing(r.Context(), stopID)
	if err != nil {
		writeError(w, http.StatusNotFound, "No briefing found for this stop — run research first")
		return
	}

	jobKey := fmt.Sprintf("lookout:%d", stop.ID)
	if !h.tryClaimJob(jobKey) {
		writeError(w, http.StatusConflict, "Safety audit already in progress for this stop")
		return
	}

	sessionID := fmt.Sprintf("lookout_%d_%d", stop.ID, time.Now().Unix())
	h.ensureProgressChannel(sessionID, 10*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"msg":        "Safety audit started",
		"stop_id":    idStr,
		"session_id": sessionID,
	})

	go func() {
		defer h.releaseJob(jobKey)
		h.ResearchSem <- struct{}{}
		defer func() { <-h.ResearchSem }()

		allStops, _ := h.DB.ListStops(context.Background(), stop.VoyageID, 0, 0)
		sort.Slice(allStops, func(i, j int) bool { return allStops[i].TargetDate.Before(allStops[j].TargetDate) })
		h.performLookoutAuditLogic(stop, briefing, allStops, sessionID)
	}()
}

func (h *Handler) TriggerVoyageLookout(w http.ResponseWriter, r *http.Request) {
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

	stops, err := h.DB.ListStops(r.Context(), voyageID, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list stops")
		return
	}

	jobKey := fmt.Sprintf("lookout:voyage:%d", voyageID)
	if !h.tryClaimJob(jobKey) {
		writeError(w, http.StatusConflict, "Safety audit already in progress for this voyage")
		return
	}

	sessionID := fmt.Sprintf("lookout_voyage_%d_%d", voyageID, time.Now().Unix())
	h.ensureProgressChannel(sessionID, 15*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"msg":        "Voyage safety audit started",
		"voyage_id":  voyageID,
		"stop_count": len(stops),
		"session_id": sessionID,
	})

	go func() {
		defer h.releaseJob(jobKey)
		ctx := context.Background()

		sort.Slice(stops, func(i, j int) bool { return stops[i].TargetDate.Before(stops[j].TargetDate) })

		var wg sync.WaitGroup
		for _, s := range stops {
			briefing, err := h.DB.GetBriefing(ctx, s.ID)
			if err != nil || briefing == nil {
				continue
			}
			stopJobKey := fmt.Sprintf("lookout:%d", s.ID)
			if !h.tryClaimJob(stopJobKey) {
				continue
			}
			wg.Add(1)
			go func(st models.Stop, b *models.Briefing, jk string) {
				defer wg.Done()
				defer h.releaseJob(jk)
				h.ResearchSem <- struct{}{}
				defer func() { <-h.ResearchSem }()
				h.performLookoutAuditLogic(&st, b, stops, sessionID)
			}(s, briefing, stopJobKey)
		}
		wg.Wait()
		h.broadcastProgress(sessionID, "done", "Voyage safety audit complete")
	}()
}

func (h *Handler) RunLookoutAuditEndpoint(w http.ResponseWriter, r *http.Request) {
	jobKey := "lookout:bulk"
	if !h.tryClaimJob(jobKey) {
		writeError(w, http.StatusConflict, "Bulk safety audit already in progress")
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Bulk safety audit started"})

	go func() {
		defer h.releaseJob(jobKey)
		ctx := context.Background()

		slog.InfoContext(ctx, "[lookout:bulk] Starting bulk safety audit")

		stops, err := h.DB.ListAllFutureStops(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "[lookout:bulk] Failed to list future stops", "error", err)
			return
		}

		slog.InfoContext(ctx, "[lookout:bulk] Future stops found", "count", len(stops))

		// Group stops by voyage so next-stop distance calculations stay within the same voyage.
		voyageStops := make(map[int64][]models.Stop)
		for _, s := range stops {
			voyageStops[s.VoyageID] = append(voyageStops[s.VoyageID], s)
		}
		for vid, vs := range voyageStops {
			sort.Slice(vs, func(i, j int) bool { return vs[i].TargetDate.Before(vs[j].TargetDate) })
			voyageStops[vid] = vs
		}

		slog.InfoContext(ctx, "[lookout:bulk] Voyages to audit", "voyage_count", len(voyageStops))

		sort.Slice(stops, func(i, j int) bool { return stops[i].TargetDate.Before(stops[j].TargetDate) })

		processed, skipped := 0, 0
		for _, s := range stops {
			briefing, err := h.DB.GetBriefing(ctx, s.ID)
			if err != nil || briefing == nil {
				slog.InfoContext(ctx, "[lookout:bulk] Skipping stop — no briefing", "stop_id", s.ID, "location", s.LocationName)
				skipped++
				continue
			}
			sessionID := fmt.Sprintf("lookout_%d_%d", s.ID, time.Now().Unix())
			stopJobKey := fmt.Sprintf("lookout:%d", s.ID)
			if !h.tryClaimJob(stopJobKey) {
				slog.InfoContext(ctx, "[lookout:bulk] Skipping stop — audit already in progress", "stop_id", s.ID, "location", s.LocationName)
				skipped++
				continue
			}
			slog.InfoContext(ctx, "[lookout:bulk] Auditing stop", "stop_id", s.ID, "location", s.LocationName, "date", s.TargetDate.Format("2006-01-02"))
			h.ResearchSem <- struct{}{}
			h.performLookoutAuditLogic(&s, briefing, voyageStops[s.VoyageID], sessionID)
			<-h.ResearchSem
			h.releaseJob(stopJobKey)
			processed++
		}
		slog.InfoContext(ctx, "[lookout:bulk] Bulk safety audit complete", "processed", processed, "skipped", skipped, "total", len(stops))
	}()
}

// performLookoutAuditLogic runs the Lookout agent for a single stop and saves alerts.
// allStops (sorted by date) is used to calculate distance to the next stop.
func (h *Handler) performLookoutAuditLogic(stop *models.Stop, briefing *models.Briefing, allStops []models.Stop, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	h.broadcastProgress(sessionID, "start", fmt.Sprintf("Auditing %s", stop.LocationName))

	// Determine stop position (1-based) and distance to the next stop.
	// The last stop has no next stop, so distNM stays 0 and no navigation alerts are generated.
	var distNM float64
	var course float64
	var hasCourse bool
	var next *models.Stop
	stopPosition, totalStops := 1, len(allStops)
	for i, s := range allStops {
		if s.ID == stop.ID {
			stopPosition = i + 1
			if i < totalStops-1 {
				next = &allStops[i+1]
				distNM = lookoutHaversineNM(stop.Latitude, stop.Longitude, next.Latitude, next.Longitude)
				course = calculateBearing(stop.Latitude, stop.Longitude, next.Latitude, next.Longitude)
				hasCourse = true
			}
			break
		}
	}

	prompt := buildLookoutPrompt(stop, briefing, distNM, course, hasCourse, stopPosition, totalStops)

	const appName = "lookout"
	const userID = "system"
	agentSessionID := fmt.Sprintf("lookout_%d_%d", stop.ID, time.Now().Unix())

	if err := h.Agent.CreateSession(ctx, appName, userID, agentSessionID, nil); err != nil {
		slog.ErrorContext(ctx, "[lookout] Failed to create session", "error", err)
		return
	}

	responseText, err := h.Agent.RunSync(ctx, appName, userID, agentSessionID, prompt)
	if err != nil {
		slog.ErrorContext(ctx, "[lookout] Agent run failed", "error", err)
		return
	}

	responseText = cleanJSON(responseText)

	var alerts []map[string]any
	if err := json.Unmarshal([]byte(responseText), &alerts); err != nil {
		slog.ErrorContext(ctx, "[lookout] Failed to parse agent response", "error", err, "raw", responseText)
		return
	}

	// Always add travel plan alert if there's a next stop
	if distNM > 0 && next != nil {
		rows := buildTravelTable(distNM, briefing.SunPhase)
		alerts = append(alerts, map[string]any{
			"severity":     "info",
			"category":     "navigation",
			"message":      fmt.Sprintf("Travel Plan: %.1f NM to %s", distNM, next.LocationName),
			"icon":         "explore",
			"travel_table": rows,
		})
	}

	alertsJSON, _ := json.Marshal(alerts)
	if err := h.DB.UpsertSafetyAlerts(ctx, stop.ID, models.RawJSON(alertsJSON)); err != nil {
		slog.ErrorContext(ctx, "[lookout] Failed to save safety alerts", "error", err)
		return
	}

	h.broadcastProgress(sessionID, "progress", fmt.Sprintf("Safety audit complete for %s", stop.LocationName))
}

func buildLookoutPrompt(stop *models.Stop, briefing *models.Briefing, distNM, course float64, hasCourse bool, stopPosition, totalStops int) string {
	distInfo := "none — this is the last stop, no departure planned (do not generate navigation or arrival-time alerts)"
	var travelTableInfo string

	if distNM > 0 {
		courseInfo := ""
		if hasCourse {
			courseInfo = fmt.Sprintf(" at a course of %.0f°", course)
		}
		distInfo = fmt.Sprintf("%.1f nautical miles%s", distNM, courseInfo)
		rows := buildTravelTable(distNM, briefing.SunPhase)
		var sb strings.Builder
		sb.WriteString("\nTravel time at various speeds (for your analysis of arrival/departure times):\n")
		for _, r := range rows {
			if r.DepartBy != "" {
				sb.WriteString(fmt.Sprintf("  %.0f kt: %s travel time (must depart by %s for safe arrival)\n", r.SpeedKt, r.TravelTime, r.DepartBy))
			} else {
				sb.WriteString(fmt.Sprintf("  %.0f kt: %s travel time\n", r.SpeedKt, r.TravelTime))
			}
		}
		travelTableInfo = sb.String()
	}

	weatherJSON := "{}"
	if len(briefing.WeatherSummary) > 0 {
		weatherJSON = string(briefing.WeatherSummary)
	}
	sunJSON := "{}"
	if len(briefing.SunPhase) > 0 {
		sunJSON = string(briefing.SunPhase)
	}
	tidesJSON := "{}"
	if len(briefing.Tides) > 0 {
		tidesJSON = string(briefing.Tides)
	}

	return fmt.Sprintf(`Analyze the following stop data for maritime safety concerns and return a JSON array of alerts.

Location: %s
Date: %s (Note: Weather and Tide data covers 48 hours starting from this date)
Stop position: %d of %d
Distance to next stop: %s%s

Weather (48h hourly forecast):
%s

Sun Phase:
%s

Tides (48h hourly forecast):
%s

Return ONLY the JSON array of alerts. If no concerns or significant trends, return [].`,
		stop.LocationName,
		stop.TargetDate.Format("January 2, 2006"),
		stopPosition,
		totalStops,
		distInfo,
		travelTableInfo,
		weatherJSON,
		sunJSON,
		tidesJSON,
	)
}

func calculateBearing(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	toDeg := func(r float64) float64 { return r * 180 / math.Pi }

	phi1 := toRad(lat1)
	phi2 := toRad(lat2)
	deltaLambda := toRad(lon2 - lon1)

	y := math.Sin(deltaLambda) * math.Cos(phi2)
	x := math.Cos(phi1)*math.Sin(phi2) - math.Sin(phi1)*math.Cos(phi2)*math.Cos(deltaLambda)
	theta := math.Atan2(y, x)

	bearing := math.Mod(toDeg(theta)+360, 360)
	return bearing
}

type TravelTableRow struct {
	SpeedKt    float64 `json:"speed_kt"`
	TravelTime string  `json:"travel_time"`
	DepartBy   string  `json:"depart_by,omitempty"`
}

// buildTravelTable generates a multi-speed travel time breakdown.
// If sunset can be parsed from sunPhaseJSON, it also shows the latest safe departure time per speed.
func buildTravelTable(distNM float64, sunPhaseJSON []byte) []TravelTableRow {
	speeds := []float64{4, 5, 6, 7, 8}

	// Try to extract sunset time from sun_phase JSON
	var sunPhase struct {
		Sunset string `json:"sunset"`
	}
	var sunsetTime time.Time
	if len(sunPhaseJSON) > 0 {
		if err := json.Unmarshal(sunPhaseJSON, &sunPhase); err == nil && sunPhase.Sunset != "" {
			// Try several common time formats
			for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "15:04", "3:04 PM"} {
				if t, err := time.Parse(layout, sunPhase.Sunset); err == nil {
					sunsetTime = t
					break
				}
			}
		}
	}

	var rows []TravelTableRow
	for _, kt := range speeds {
		hours := distNM / kt
		h := int(hours)
		m := int((hours - float64(h)) * 60)
		row := TravelTableRow{
			SpeedKt:    kt,
			TravelTime: fmt.Sprintf("%dh %02dm", h, m),
		}

		if !sunsetTime.IsZero() {
			// Latest departure to arrive 30 min before sunset
			safeArrival := sunsetTime.Add(-30 * time.Minute)
			depart := safeArrival.Add(-time.Duration(hours * float64(time.Hour)))
			row.DepartBy = depart.Format("15:04")
		}
		rows = append(rows, row)
	}

	return rows
}

func lookoutHaversineNM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 3440.065
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}
