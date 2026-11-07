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

	"app/internal/model"
	"app/internal/safety"
)

func (h *Handler) TriggerLookoutAudit(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	dayID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Day ID")
		return
	}

	day, err := h.DB.GetDay(r.Context(), dayID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Day not found")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), day.VoyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	briefing, err := h.DB.GetBriefing(r.Context(), dayID)
	if err != nil {
		writeError(w, http.StatusNotFound, "No briefing found for this day — run research first")
		return
	}

	jobKey := fmt.Sprintf("lookout:%d", day.ID)
	if !h.tryClaimJob(jobKey) {
		writeError(w, http.StatusConflict, "Safety audit already in progress for this day")
		return
	}

	sessionID := fmt.Sprintf("lookout_%d_%d", day.ID, time.Now().Unix())
	h.ensureProgressChannel(sessionID, 10*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"msg":        "Safety audit started",
		"day_id":     idStr,
		"stop_id":    idStr,
		"session_id": sessionID,
	})

	go func() {
		defer h.releaseJob(jobKey)
		h.ResearchSem <- struct{}{}
		defer func() { <-h.ResearchSem }()

		ctx := context.Background()
		h.broadcastProgress(sessionID, "weather", fmt.Sprintf("Refreshing weather forecast for %s", day.PrimaryLocationName()))
		if ws, err := fetchWeatherForDay(ctx, *day); err == nil {
			if err := h.DB.UpsertWeatherBriefing(ctx, day.ID, ws); err == nil {
				if wsBytes, err := json.Marshal(ws); err == nil {
					briefing.WeatherSummary = model.RawJSON(wsBytes)
				}
			} else {
				slog.WarnContext(ctx, "[lookout] Failed to save refreshed weather before audit", "day_id", day.ID, "error", err)
			}
		} else {
			slog.WarnContext(ctx, "[lookout] Failed to refresh weather before audit", "day_id", day.ID, "error", err)
		}

		allDays, _ := h.DB.ListDays(ctx, day.VoyageID, 0, 0)
		sort.Slice(allDays, func(i, j int) bool { return allDays[i].TargetDate.Before(allDays[j].TargetDate) })
		h.performLookoutAuditLogic(day, briefing, allDays, sessionID)
	}()
}

func (h *Handler) TriggerVoyageLookout(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := voyage.ID

	days, err := h.DB.ListDays(r.Context(), voyageID, 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list days")
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
		"day_count":  len(days),
		"stop_count": len(days),
		"session_id": sessionID,
	})

	go func() {
		defer h.releaseJob(jobKey)
		ctx := context.Background()

		sort.Slice(days, func(i, j int) bool { return days[i].TargetDate.Before(days[j].TargetDate) })

		var wg sync.WaitGroup
		for _, d := range days {
			briefing, err := h.DB.GetBriefing(ctx, d.ID)
			if err != nil || briefing == nil {
				continue
			}
			dayJobKey := fmt.Sprintf("lookout:%d", d.ID)
			if !h.tryClaimJob(dayJobKey) {
				continue
			}
			wg.Add(1)
			go func(dy model.VoyageDay, b *model.Briefing, jk string) {
				defer wg.Done()
				defer h.releaseJob(jk)
				h.ResearchSem <- struct{}{}
				defer func() { <-h.ResearchSem }()

				if b.WeatherLastUpdated == nil || time.Since(*b.WeatherLastUpdated) > time.Minute {
					if ws, err := fetchWeatherForDay(ctx, dy); err == nil {
						if err := h.DB.UpsertWeatherBriefing(ctx, dy.ID, ws); err == nil {
							if wsBytes, err := json.Marshal(ws); err == nil {
								b.WeatherSummary = model.RawJSON(wsBytes)
							}
						}
					}
				}
				h.performLookoutAuditLogic(&dy, b, days, sessionID)
			}(d, briefing, dayJobKey)
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

		days, err := h.DB.ListAllFutureDays(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "[lookout:bulk] Failed to list future days", "error", err)
			return
		}

		slog.InfoContext(ctx, "[lookout:bulk] Future days found", "count", len(days))

		voyageDays := make(map[int64][]model.VoyageDay)
		for _, d := range days {
			voyageDays[d.VoyageID] = append(voyageDays[d.VoyageID], d)
		}
		for vid, vd := range voyageDays {
			sort.Slice(vd, func(i, j int) bool { return vd[i].TargetDate.Before(vd[j].TargetDate) })
			voyageDays[vid] = vd
		}

		slog.InfoContext(ctx, "[lookout:bulk] Voyages to audit", "voyage_count", len(voyageDays))

		sort.Slice(days, func(i, j int) bool { return days[i].TargetDate.Before(days[j].TargetDate) })

		processed, skipped := 0, 0
		for _, d := range days {
			briefing, err := h.DB.GetBriefing(ctx, d.ID)
			if err != nil || briefing == nil {
				slog.InfoContext(ctx, "[lookout:bulk] Skipping day — no briefing", "day_id", d.ID, "location", d.PrimaryLocationName())
				skipped++
				continue
			}
			sessionID := fmt.Sprintf("lookout_%d_%d", d.ID, time.Now().Unix())
			dayJobKey := fmt.Sprintf("lookout:%d", d.ID)
			if !h.tryClaimJob(dayJobKey) {
				slog.InfoContext(ctx, "[lookout:bulk] Skipping day — audit already in progress", "day_id", d.ID, "location", d.PrimaryLocationName())
				skipped++
				continue
			}
			slog.InfoContext(ctx, "[lookout:bulk] Auditing day", "day_id", d.ID, "location", d.PrimaryLocationName(), "date", d.TargetDate.Format("2006-01-02"))
			h.ResearchSem <- struct{}{}
			h.performLookoutAuditLogic(&d, briefing, voyageDays[d.VoyageID], sessionID)
			<-h.ResearchSem
			h.releaseJob(dayJobKey)
			processed++
		}
		slog.InfoContext(ctx, "[lookout:bulk] Bulk safety audit complete", "processed", processed, "skipped", skipped, "total", len(days))
	}()
}

func (h *Handler) performLookoutAuditLogic(day *model.VoyageDay, briefing *model.Briefing, allDays []model.VoyageDay, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	destName := day.PrimaryLocationName()
	h.broadcastProgress(sessionID, "start", fmt.Sprintf("Auditing %s", destName))

	var distNM float64
	var course float64
	var hasCourse bool
	dayPosition, totalDays := 1, len(allDays)
	if totalDays == 0 {
		totalDays = 1
	}
	for i, d := range allDays {
		if d.ID == day.ID {
			dayPosition = i + 1
			break
		}
	}

	if (day.StartLatitude != 0 || day.StartLongitude != 0) && (day.EndLatitude != 0 || day.EndLongitude != 0) {
		distNM = lookoutHaversineNM(day.StartLatitude, day.StartLongitude, day.EndLatitude, day.EndLongitude)
		if distNM > 0.1 {
			course = calculateBearing(day.StartLatitude, day.StartLongitude, day.EndLatitude, day.EndLongitude)
			hasCourse = true
		} else {
			distNM = 0
		}
	}

	var plannedRouteInfo string
	matchedPlannedTracks := h.findPlannedTracksForDay(ctx, day, dayPosition)
	var matchedPlannedTrack *model.VoyageTrack
	if len(matchedPlannedTracks) > 0 {
		matchedPlannedTrack = &matchedPlannedTracks[0]
		plannedRouteInfo, distNM = formatPlannedRoutesForLookout(matchedPlannedTracks, distNM)
		if alongEnv := formatAlongRouteEnvForLookout(matchedPlannedTracks, briefing); alongEnv != "" {
			plannedRouteInfo = strings.TrimSpace(plannedRouteInfo) + "\n\n" + alongEnv + "\n"
		}
	}

	prompt := buildLookoutPrompt(day, briefing, distNM, course, hasCourse, dayPosition, totalDays, plannedRouteInfo)

	const appName = "lookout"
	const userID = "system"
	agentSessionID := fmt.Sprintf("lookout_%d_%d", day.ID, time.Now().Unix())

	if err := h.Agent.CreateSession(ctx, appName, userID, agentSessionID, nil); err != nil {
		slog.WarnContext(ctx, "[lookout] CreateSession warning, proceeding with RunSync", "error", err)
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

	for i := range alerts {
		if rawCat, ok := alerts[i]["category"].(string); ok {
			alerts[i]["category"] = normalizeLookoutAlertCategory(rawCat)
		} else {
			alerts[i]["category"] = "navigation"
		}
		if rawIcon, ok := alerts[i]["icon"].(string); ok {
			alerts[i]["icon"] = normalizeLookoutAlertIcon(rawIcon)
		} else {
			alerts[i]["icon"] = "warning"
		}
	}

	alerts = ensurePilotNoteLNMAlerts(alerts, briefing.PilotNotes)

	if distNM > 0 {
		rows := buildTravelTable(distNM, briefing.SunPhase)
		planMsg := fmt.Sprintf("Travel Plan: %.1f NM to %s", distNM, destName)
		if matchedPlannedTrack != nil && matchedPlannedTrack.Name != "" {
			planMsg = fmt.Sprintf("Travel Plan: %.1f NM via %s to %s", distNM, matchedPlannedTrack.Name, destName)
		}
		alerts = append(alerts, map[string]any{
			"severity":     "info",
			"category":     "navigation",
			"message":      planMsg,
			"icon":         "explore",
			"travel_table": rows,
		})
	}

	alertsJSON, _ := json.Marshal(alerts)
	if err := h.DB.UpsertSafetyAlerts(ctx, day.ID, model.RawJSON(alertsJSON)); err != nil {
		slog.ErrorContext(ctx, "[lookout] Failed to save safety alerts", "error", err)
		return
	}

	h.broadcastProgressSection(sessionID, "alerts", fmt.Sprintf("Safety lookout alerts ready for %s", destName), alertsJSON)
	h.broadcastProgress(sessionID, "progress", fmt.Sprintf("Safety audit complete for %s", destName))
}

func extractCoordinates(raw model.RawJSON) [][]float64 {
	if len(raw) == 0 {
		return nil
	}
	var feat struct {
		Geometry struct {
			Type        string      `json:"type"`
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
	}
	if err := json.Unmarshal(raw, &feat); err == nil && len(feat.Geometry.Coordinates) > 0 {
		return feat.Geometry.Coordinates
	}
	var geom struct {
		Type        string      `json:"type"`
		Coordinates [][]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &geom); err == nil && len(geom.Coordinates) > 0 {
		return geom.Coordinates
	}
	var fc struct {
		Features []struct {
			Geometry struct {
				Type        string      `json:"type"`
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &fc); err == nil {
		var allCoords [][]float64
		for _, f := range fc.Features {
			if len(f.Geometry.Coordinates) > 0 {
				allCoords = append(allCoords, f.Geometry.Coordinates...)
			}
		}
		if len(allCoords) > 0 {
			return allCoords
		}
	}
	return nil
}

func bearingToCardinal(deg float64) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	normalized := math.Mod(deg, 360)
	if normalized < 0 {
		normalized += 360
	}
	idx := int(math.Floor((normalized+11.25)/22.5)) % 16
	return dirs[idx]
}

func (h *Handler) findPlannedTrackForDay(ctx context.Context, day *model.VoyageDay, dayPosition int) *model.VoyageTrack {
	matched := h.findPlannedTracksForDay(ctx, day, dayPosition)
	if len(matched) == 0 {
		return nil
	}
	return &matched[0]
}

func (h *Handler) findPlannedTracksForDay(ctx context.Context, day *model.VoyageDay, dayPosition int) []model.VoyageTrack {
	if day == nil {
		return nil
	}
	tracks, err := h.DB.ListVoyageTracks(ctx, day.VoyageID)
	if err != nil {
		slog.WarnContext(ctx, "[lookout] Failed to list voyage tracks", "voyage_id", day.VoyageID, "day_id", day.ID, "error", err)
		tracks = nil
	}

	var plannedTracks []model.VoyageTrack
	for _, tr := range tracks {
		if tr.Kind == string(model.TrackKindPlanned) {
			plannedTracks = append(plannedTracks, tr)
		}
	}

	if len(plannedTracks) == 0 {
		if dayTracks, err := h.DB.ListDayTracks(ctx, day.ID); err == nil {
			for _, tr := range dayTracks {
				if tr.Kind == string(model.TrackKindPlanned) {
					plannedTracks = append(plannedTracks, tr)
				}
			}
		}
	}

	if len(plannedTracks) == 0 {
		return nil
	}

	hasSplitLegs := false
	for _, tr := range plannedTracks {
		if tr.VoyageDayID != nil || extractLegNumber(tr.Name) > 0 {
			hasSplitLegs = true
			break
		}
	}

	candidateTracks := make([]model.VoyageTrack, 0, len(plannedTracks))
	for _, tr := range plannedTracks {
		if hasSplitLegs && isRedundantUnsplitMasterTrack(tr, plannedTracks, day) {
			continue
		}
		candidateTracks = append(candidateTracks, tr)
	}

	if len(candidateTracks) == 0 {
		return nil
	}

	hasDayCoords := (day.StartLatitude != 0 || day.StartLongitude != 0) && (day.EndLatitude != 0 || day.EndLongitude != 0)
	var matched []model.VoyageTrack
	seenIDs := make(map[string]bool)
	seenSigs := make(map[string]bool)
	addMatch := func(tr model.VoyageTrack) {
		if tr.ID != "" {
			if seenIDs[tr.ID] {
				return
			}
			seenIDs[tr.ID] = true
		}
		sig := plannedTrackGeometrySignature(&tr)
		if sig != "" {
			if seenSigs[sig] {
				return
			}
			seenSigs[sig] = true
		}
		matched = append(matched, tr)
	}

	for i := range candidateTracks {
		tr := candidateTracks[i]
		// (a) Direct match on day ID
		if day.ID != 0 && tr.VoyageDayID != nil && *tr.VoyageDayID == day.ID {
			addMatch(tr)
			continue
		}
		// Skip tracks explicitly bound to a different day ID
		if day.ID != 0 && tr.VoyageDayID != nil && *tr.VoyageDayID != day.ID {
			continue
		}

		// (b) Match on Leg number in track name (e.g. "Leg 1" or "Plan A - Leg 1")
		legNum := extractLegNumber(tr.Name)
		if legNum > 0 {
			if legNum == dayPosition {
				addMatch(tr)
			}
			continue
		}

		// (c) Match on start & end endpoint proximity (within 3.0 NM)
		if hasDayCoords {
			startLat, startLng, endLat, endLng, ok := extractTrackEndpoints(&tr)
			if ok {
				dStart := lookoutHaversineNM(startLat, startLng, day.StartLatitude, day.StartLongitude)
				dEnd := lookoutHaversineNM(endLat, endLng, day.EndLatitude, day.EndLongitude)
				if dStart <= 3.0 && dEnd <= 3.0 {
					addMatch(tr)
				}
			}
		}
	}

	if len(matched) > 0 {
		return matched
	}

	if hasSplitLegs {
		return nil
	}

	// 4. If exactly 1 planned track and Day 1
	if len(candidateTracks) == 1 && dayPosition == 1 {
		return []model.VoyageTrack{candidateTracks[0]}
	}

	// 5. Sequential index fallback
	legIdx := dayPosition - 1
	if legIdx >= 0 && legIdx < len(candidateTracks) {
		return []model.VoyageTrack{candidateTracks[legIdx]}
	}

	return nil
}

func isRedundantUnsplitMasterTrack(tr model.VoyageTrack, allPlanned []model.VoyageTrack, day *model.VoyageDay) bool {
	if tr.VoyageDayID != nil || extractLegNumber(tr.Name) > 0 {
		return false
	}
	trName := strings.ToLower(strings.TrimSpace(tr.Name))
	for _, other := range allPlanned {
		if other.ID == tr.ID && tr.ID != "" {
			continue
		}
		if other.VoyageDayID == nil && extractLegNumber(other.Name) == 0 {
			continue
		}
		if tr.FileName != nil && *tr.FileName != "" && other.FileName != nil && *other.FileName == *tr.FileName {
			return true
		}
		otherName := strings.ToLower(strings.TrimSpace(other.Name))
		if trName != "" && strings.HasPrefix(otherName, trName+" - leg ") {
			return true
		}
	}
	if trName == "" || trName == "waypoints route" || trName == "untitled" ||
		strings.HasPrefix(trName, "track ") || strings.HasPrefix(trName, "route ") ||
		strings.Contains(trName, "master") || strings.Contains(trName, "whole") ||
		strings.Contains(trName, "full") || strings.Contains(trName, "voyage") {
		return true
	}
	if day == nil || (day.StartLatitude == 0 && day.StartLongitude == 0) || (day.EndLatitude == 0 && day.EndLongitude == 0) {
		return true
	}
	startLat, startLng, endLat, endLng, ok := extractTrackEndpoints(&tr)
	if !ok {
		return true
	}
	dStart := lookoutHaversineNM(startLat, startLng, day.StartLatitude, day.StartLongitude)
	dEnd := lookoutHaversineNM(endLat, endLng, day.EndLatitude, day.EndLongitude)
	if dStart > 3.0 || dEnd > 3.0 {
		return true
	}
	if tr.DistanceNM != nil && *tr.DistanceNM > 0 {
		for _, other := range allPlanned {
			if (other.VoyageDayID != nil && *other.VoyageDayID == day.ID) || extractLegNumber(other.Name) > 0 {
				if other.DistanceNM != nil && *other.DistanceNM > 0 && *tr.DistanceNM > *other.DistanceNM*1.8 {
					return true
				}
			}
		}
	}
	return false
}

func formatPlannedRoutesForLookout(tracks []model.VoyageTrack, fallbackDist float64) (string, float64) {
	if len(tracks) == 0 {
		return "", fallbackDist
	}
	if len(tracks) == 1 {
		return formatPlannedRouteForLookout(&tracks[0], fallbackDist)
	}

	var sb strings.Builder
	primaryDist := fallbackDist
	havePrimaryDist := false

	sb.WriteString(fmt.Sprintf("Multiple Candidate Planned Routes (%d options available for this leg):\n\n", len(tracks)))
	for i := range tracks {
		tr := &tracks[i]
		routeBlock, routeDist := formatPlannedRouteForLookout(tr, fallbackDist)
		if !havePrimaryDist && routeDist > 0 {
			primaryDist = routeDist
			havePrimaryDist = true
		}
		routeName := strings.TrimSpace(tr.Name)
		if routeName == "" {
			routeName = fmt.Sprintf("Plan %d", i+1)
		}
		sb.WriteString(fmt.Sprintf("=== Candidate Planned Route %d of %d: %s ===\n", i+1, len(tracks), routeName))
		if routeBlock != "" {
			sb.WriteString(routeBlock)
		} else if routeDist > 0 {
			sb.WriteString(fmt.Sprintf("Intended Planned Route: %q (Distance: %.1f NM)\n", routeName, routeDist))
		}
		if routeDist > 0 {
			sb.WriteString(fmt.Sprintf("Travel Time Table for %q (%.1f NM):\n", routeName, routeDist))
			for _, kt := range []float64{5, 6, 7, 8} {
				hours := routeDist / kt
				h := int(hours)
				m := int(math.Round((hours - float64(h)) * 60))
				if m == 60 {
					h++
					m = 0
				}
				sb.WriteString(fmt.Sprintf("  * %.0f kts: %dh %02dm\n", kt, h, m))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Multi-Plan Comparison Instructions:\n")
	sb.WriteString("Compare all candidate planned routes above against:\n")
	sb.WriteString("  - Wind angles (true/apparent wind relative to each route's segment bearings)\n")
	sb.WriteString("  - Wave exposure, swell alignment, and open-water fetch vs. sheltered waters\n")
	sb.WriteString("  - Tidal currents, slack water windows, and tidal gates along each corridor\n")
	sb.WriteString("  - Daylight margin and arrival time before sunset based on each route's travel time table\n")
	sb.WriteString("  - Hydrographic hazards, shoals, narrow passes, bridges, and Local Notices to Mariners\n")
	sb.WriteString("Explicitly name each candidate route and recommend the safer or more favorable route for the forecasted conditions.\n")

	return sb.String(), primaryDist
}

func formatPlannedRouteForLookout(tr *model.VoyageTrack, fallbackDist float64) (string, float64) {
	if tr == nil {
		return "", fallbackDist
	}

	distNM := fallbackDist
	if tr.DistanceNM != nil && *tr.DistanceNM > 0 {
		distNM = *tr.DistanceNM
	}

	raw := tr.SimplifiedGeoJSON
	if len(raw) == 0 {
		raw = tr.GeoJSON
	}
	coords := extractCoordinates(raw)

	if distNM <= 0 && len(coords) >= 2 {
		var calcDist float64
		for i := 0; i < len(coords)-1; i++ {
			calcDist += lookoutHaversineNM(coords[i][1], coords[i][0], coords[i+1][1], coords[i+1][0])
		}
		if calcDist > 0 {
			distNM = calcDist
		}
	}

	routeName := strings.TrimSpace(tr.Name)
	if routeName == "" {
		routeName = "Planned Route"
	}

	if len(coords) < 2 {
		if tr.Name != "" {
			return fmt.Sprintf("Intended Planned Route: %q (Distance: %.1f NM)\n", routeName, distNM), distNM
		}
		return "", distNM
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Intended Planned Route: %q (Total Distance: %.1f NM)\n", routeName, distNM))

	numPts := len(coords)
	step := 1
	if numPts > 8 {
		step = numPts / 6
		if step < 1 {
			step = 1
		}
	}

	var sampleIdxs []int
	for i := 0; i < numPts; i += step {
		sampleIdxs = append(sampleIdxs, i)
	}
	if sampleIdxs[len(sampleIdxs)-1] != numPts-1 {
		sampleIdxs = append(sampleIdxs, numPts-1)
	}

	sb.WriteString("Key Route Segments & Bearings:\n")
	for s := 0; s < len(sampleIdxs)-1; s++ {
		idx1 := sampleIdxs[s]
		idx2 := sampleIdxs[s+1]
		lat1, lon1 := coords[idx1][1], coords[idx1][0]
		lat2, lon2 := coords[idx2][1], coords[idx2][0]
		segDist := lookoutHaversineNM(lat1, lon1, lat2, lon2)
		segBearing := calculateBearing(lat1, lon1, lat2, lon2)
		segCardinal := bearingToCardinal(segBearing)
		sb.WriteString(fmt.Sprintf("  * Segment %d: %.1f NM on course %.0f° (%s) from (%.4f, %.4f) to (%.4f, %.4f)\n",
			s+1, segDist, segBearing, segCardinal, lat1, lon1, lat2, lon2))
	}

	polySummary := extractWaypointsSummary(raw)
	if polySummary != "" {
		sb.WriteString(fmt.Sprintf("Full Planned Waypoints Sequence:\n  %s\n", polySummary))
	}

	return sb.String(), distNM
}

func extractWaypointsSummary(geoJSON model.RawJSON) string {
	coords := extractCoordinates(geoJSON)
	if len(coords) == 0 {
		return ""
	}
	step := 1
	if len(coords) > 10 {
		step = len(coords) / 10
	}
	var pts []string
	for i := 0; i < len(coords); i += step {
		if len(coords[i]) >= 2 {
			pts = append(pts, fmt.Sprintf("(%.4f, %.4f)", coords[i][1], coords[i][0]))
		}
	}
	last := coords[len(coords)-1]
	if len(last) >= 2 {
		lastStr := fmt.Sprintf("(%.4f, %.4f)", last[1], last[0])
		if len(pts) == 0 || pts[len(pts)-1] != lastStr {
			pts = append(pts, lastStr)
		}
	}
	return strings.Join(pts, " -> ")
}

func buildLookoutPrompt(day *model.VoyageDay, briefing *model.Briefing, distNM, course float64, hasCourse bool, dayPosition, totalDays int, routeWaypoints ...string) string {
	distInfo := "none — stationary day or final harbor stay, no transit leg planned (do not generate navigation or arrival-time alerts)"
	var travelTableInfo string
	var nextStopInfo string

	startLoc := day.StartLocationName
	if startLoc == "" {
		startLoc = day.EndLocationName
	}
	endLoc := day.EndLocationName
	if endLoc == "" {
		endLoc = startLoc
	}

	if distNM > 0 {
		courseInfo := ""
		if hasCourse {
			courseInfo = fmt.Sprintf(" at a course of %.0f°", course)
		}
		if endLoc != "" {
			destFmt := endLoc
			if day.EndLatitude != 0 || day.EndLongitude != 0 {
				destFmt = fmt.Sprintf("%s (%.4f, %.4f)", endLoc, day.EndLatitude, day.EndLongitude)
			}
			nextStopInfo = fmt.Sprintf("\nDestination: %s", destFmt)
			distInfo = fmt.Sprintf("%.1f nautical miles%s to %s", distNM, courseInfo, endLoc)
		} else {
			distInfo = fmt.Sprintf("%.1f nautical miles%s", distNM, courseInfo)
		}
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

	constraintsBlock := formatSafetyConstraints(safety.Evaluate(briefing.WeatherSummary, briefing.Tides))

	locationInfo := startLoc
	if day.StartLatitude != 0 || day.StartLongitude != 0 {
		locationInfo = fmt.Sprintf("%s (%.4f, %.4f)", startLoc, day.StartLatitude, day.StartLongitude)
	} else if day.EndLatitude != 0 || day.EndLongitude != 0 {
		locationInfo = fmt.Sprintf("%s (%.4f, %.4f)", endLoc, day.EndLatitude, day.EndLongitude)
	}

	var routeWaypointsInfo string
	if len(routeWaypoints) > 0 && routeWaypoints[0] != "" {
		routeWaypointsInfo = fmt.Sprintf("\n%s\n", strings.TrimSpace(routeWaypoints[0]))
	}
	if len(briefing.PilotNotes) > 0 && string(briefing.PilotNotes) != "null" && string(briefing.PilotNotes) != "{}" {
		routeWaypointsInfo += fmt.Sprintf("\nPilot Notes & Local Notices to Mariners (surface any valid local_notices as navigation alerts):\n%s\n", strings.TrimSpace(string(briefing.PilotNotes)))
	}

	return fmt.Sprintf(`Analyze the following voyage day data, planned passage route (if provided), and official hydrographic publications for maritime safety concerns, hazards along the route, and local recommendations. When an intended planned route is provided, evaluate safety, tidal streams, and adverse weather against the route's specific headings and waypoints. Return a JSON array of alerts.

Departure Location: %s
Date: %s (Note: Weather and Tide data covers 48 hours starting from this date)
Day position: %d of %d%s
Transit distance: %s%s%s

Weather (48h hourly forecast):
%s

Sun Phase:
%s

Tides (48h hourly forecast):
%s

%s

Return ONLY the JSON array of alerts. If no concerns or significant trends, return [].`,
		locationInfo,
		day.TargetDate.Format("January 2, 2006"),
		dayPosition,
		totalDays,
		nextStopInfo,
		distInfo,
		travelTableInfo,
		routeWaypointsInfo,
		weatherJSON,
		sunJSON,
		tidesJSON,
		constraintsBlock,
	)
}

func formatSafetyConstraints(alerts []safety.Alert) string {
	const header = "Pre-evaluated safety constraints (computed deterministically from the forecast above — treat as AUTHORITATIVE: include each of these alerts in your output as-is, and do NOT recompute wind, wave, wave-period, or tidal-range numeric thresholds yourself):"
	if len(alerts) == 0 {
		return header + "\n[] (deterministic wind/wave/wave-period/tide checks ran and triggered no alerts)"
	}
	b, err := json.Marshal(alerts)
	if err != nil {
		return header + "\n[]"
	}
	return header + "\n" + string(b)
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

func buildTravelTable(distNM float64, sunPhaseJSON []byte) []TravelTableRow {
	speeds := []float64{4, 5, 6, 7, 8}

	var sunPhase struct {
		Sunset string `json:"sunset"`
	}
	var sunsetTime time.Time
	if len(sunPhaseJSON) > 0 {
		if err := json.Unmarshal(sunPhaseJSON, &sunPhase); err == nil && sunPhase.Sunset != "" {
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

func normalizeLookoutAlertIcon(icon string) string {
	clean := strings.ToLower(strings.TrimSpace(icon))
	clean = strings.ReplaceAll(clean, "-", "_")
	clean = strings.ReplaceAll(clean, " ", "_")
	switch clean {
	case "bridge", "bridges", "clearance", "vertical_clearance", "overhead", "mast":
		return "height"
	case "boat", "vessel", "ship":
		return "directions_boat"
	case "sail":
		return "sailing"
	case "compass", "navigation":
		return "explore"
	case "current", "currents", "cross_current":
		return "water"
	case "tide", "tides", "rip", "rips", "tidal_rip", "rough_seas":
		return "waves"
	case "shoal", "shallow", "rock", "reef", "alert", "caution":
		return "warning"
	case "depth", "channel":
		return "straighten"
	case "wind", "gust", "gale":
		return "air"
	case "thunderstorm", "lightning":
		return "bolt"
	case "rain":
		return "rainy"
	case "snow":
		return "weather_snowy"
	case "fog":
		return "foggy"
	case "temperature":
		return "thermostat"
	case "sun", "daylight":
		return "light_mode"
	case "sunset", "sunrise", "twilight":
		return "wb_twilight"
	case "time", "clock":
		return "schedule"
	case "danger":
		return "dangerous"
	case "notice":
		return "info"
	case "":
		return "warning"
	default:
		return clean
	}
}

func normalizeLookoutAlertCategory(cat string) string {
	clean := strings.ToLower(strings.TrimSpace(cat))
	switch clean {
	case "weather", "waves", "wave", "wind", "storm", "sea_state", "seas", "swell":
		return "weather"
	case "tides", "tide", "current", "currents", "tidal_rip":
		return "tides"
	case "sun", "daylight", "sunset", "sunrise", "twilight":
		return "sun"
	case "navigation", "pilotage", "hazard", "hazards", "route", "channel":
		return "navigation"
	default:
		return "navigation"
	}
}

var lnmStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"through": true, "into": true, "under": true, "are": true, "all": true,
	"lnm": true, "uscg": true, "local": true, "notice": true, "mariners": true,
	"verify": true, "current": true, "status": true, "latest": true,
	"before": true, "transiting": true, "reported": true, "operations": true,
	"san": true, "francisco": true, "bay": true, "california": true,
	"united": true, "states": true, "aton": true, "discrepancies": true,
}

func extractLNMContentTokens(text string) []string {
	lower := strings.ToLower(text)
	var tokenBuf strings.Builder
	var tokens []string
	seen := make(map[string]bool)
	flush := func() {
		if tokenBuf.Len() == 0 {
			return
		}
		tok := tokenBuf.String()
		tokenBuf.Reset()
		hasDigit := false
		for _, r := range tok {
			if r >= '0' && r <= '9' {
				hasDigit = true
				break
			}
		}
		if (!hasDigit && len(tok) < 4) || lnmStopWords[tok] || seen[tok] {
			return
		}
		seen[tok] = true
		tokens = append(tokens, tok)
	}
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			tokenBuf.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func lnmTokensOverlap(tokensA, tokensB []string) bool {
	if len(tokensA) == 0 || len(tokensB) == 0 {
		return false
	}
	setB := make(map[string]bool, len(tokensB))
	for _, t := range tokensB {
		setB[t] = true
	}
	shared := 0
	for _, t := range tokensA {
		if setB[t] {
			shared++
		}
	}
	if shared >= 4 {
		return true
	}
	minLen := len(tokensA)
	if len(tokensB) < minLen {
		minLen = len(tokensB)
	}
	return minLen > 0 && float64(shared)/float64(minLen) >= 0.5
}

// ensurePilotNoteLNMAlerts guarantees that any valid Local Notice to Mariners (LNM)
// entries stored in briefing.PilotNotes.local_notices are surfaced as navigation
// safety alerts if the LLM did not already emit them.
func ensurePilotNoteLNMAlerts(alerts []map[string]any, pilotNotesRaw []byte) []map[string]any {
	if len(pilotNotesRaw) == 0 {
		return deduplicateLookoutAlerts(alerts)
	}
	var pn struct {
		LocalNotices []string `json:"local_notices"`
	}
	if err := json.Unmarshal(pilotNotesRaw, &pn); err != nil || len(pn.LocalNotices) == 0 {
		return deduplicateLookoutAlerts(alerts)
	}

	var existingText strings.Builder
	var existingTokenSets [][]string
	for _, a := range alerts {
		msg, _ := a["message"].(string)
		act, _ := a["action"].(string)
		existingText.WriteString(strings.ToLower(msg))
		existingText.WriteByte(' ')
		existingTokenSets = append(existingTokenSets, extractLNMContentTokens(msg+" "+act))
	}
	combinedExisting := existingText.String()

	for _, rawNotice := range pn.LocalNotices {
		notice := strings.TrimSpace(rawNotice)
		if notice == "" {
			continue
		}
		lower := strings.ToLower(notice)
		if strings.Contains(lower, "none") && len(notice) < 25 {
			continue
		}
		// Check if already covered by checking a distinctive substring or token overlap
		checkSnippet := lower
		if len(checkSnippet) > 32 {
			checkSnippet = checkSnippet[:32]
		}
		if checkSnippet != "" && strings.Contains(combinedExisting, checkSnippet) {
			continue
		}
		noticeTokens := extractLNMContentTokens(notice)
		alreadyCovered := false
		for _, exTokens := range existingTokenSets {
			if lnmTokensOverlap(noticeTokens, exTokens) {
				alreadyCovered = true
				break
			}
		}
		if alreadyCovered {
			continue
		}
		msg := notice
		if !strings.HasPrefix(strings.ToUpper(msg), "LNM") && !strings.Contains(lower, "notice to mariners") {
			msg = "LNM: " + msg
		}
		alerts = append(alerts, map[string]any{
			"severity": "warning",
			"category": "navigation",
			"message":  msg,
			"action":   "Verify current status in the latest USCG Local Notice to Mariners before transiting.",
			"icon":     "warning",
		})
		existingTokenSets = append(existingTokenSets, noticeTokens)
	}
	return deduplicateLookoutAlerts(alerts)
}

func deduplicateLookoutAlerts(alerts []map[string]any) []map[string]any {
	if len(alerts) <= 1 {
		return alerts
	}
	var out []map[string]any
	var outTokens [][]string
	for _, a := range alerts {
		msg, _ := a["message"].(string)
		act, _ := a["action"].(string)
		lowerMsg := strings.ToLower(strings.TrimSpace(msg))
		isLNM := strings.HasPrefix(lowerMsg, "lnm:") || strings.Contains(lowerMsg, "local notice to mariners")
		tokens := extractLNMContentTokens(msg + " " + act)
		dupIdx := -1
		for i, exTokens := range outTokens {
			exMsg, _ := out[i]["message"].(string)
			exLower := strings.ToLower(strings.TrimSpace(exMsg))
			if exLower == lowerMsg {
				dupIdx = i
				break
			}
			exIsLNM := strings.HasPrefix(exLower, "lnm:") || strings.Contains(exLower, "local notice to mariners")
			if isLNM && exIsLNM && lnmTokensOverlap(tokens, exTokens) {
				dupIdx = i
				break
			}
		}
		if dupIdx >= 0 {
			exAct, _ := out[dupIdx]["action"].(string)
			if strings.Contains(strings.ToLower(exAct), "verify current status in the latest uscg local notice") &&
				!strings.Contains(strings.ToLower(act), "verify current status in the latest uscg local notice") {
				out[dupIdx] = a
				outTokens[dupIdx] = tokens
			}
			continue
		}
		out = append(out, a)
		outTokens = append(outTokens, tokens)
	}
	return out
}
