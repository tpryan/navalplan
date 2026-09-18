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
)

func (h *Handler) TriggerLookoutAudit(w http.ResponseWriter, r *http.Request) {
	person := GetPersonFromContext(r.Context())
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
	person := GetPersonFromContext(r.Context())
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
			go func(st model.Stop, b *model.Briefing, jk string) {
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

		voyageStops := make(map[int64][]model.Stop)
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

func (h *Handler) performLookoutAuditLogic(stop *model.Stop, briefing *model.Briefing, allStops []model.Stop, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	h.broadcastProgress(sessionID, "start", fmt.Sprintf("Auditing %s", stop.LocationName))

	var distNM float64
	var course float64
	var hasCourse bool
	var next *model.Stop
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

	var plannedRouteInfo string
	var matchedPlannedTrack *model.VoyageTrack
	if next != nil {
		matchedPlannedTrack = h.findPlannedTrackForLeg(ctx, stop, next, stopPosition)
		if matchedPlannedTrack != nil {
			plannedRouteInfo, distNM = formatPlannedRouteForLookout(matchedPlannedTrack, distNM)
		}
	}

	prompt := buildLookoutPrompt(stop, next, briefing, distNM, course, hasCourse, stopPosition, totalStops, plannedRouteInfo)

	const appName = "lookout"
	const userID = "system"
	agentSessionID := fmt.Sprintf("lookout_%d_%d", stop.ID, time.Now().Unix())

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
		if rawIcon, ok := alerts[i]["icon"].(string); ok {
			alerts[i]["icon"] = normalizeLookoutAlertIcon(rawIcon)
		} else {
			alerts[i]["icon"] = "warning"
		}
	}

	if distNM > 0 && next != nil {
		rows := buildTravelTable(distNM, briefing.SunPhase)
		planMsg := fmt.Sprintf("Travel Plan: %.1f NM to %s", distNM, next.LocationName)
		if matchedPlannedTrack != nil && matchedPlannedTrack.Name != "" {
			planMsg = fmt.Sprintf("Travel Plan: %.1f NM via %s to %s", distNM, matchedPlannedTrack.Name, next.LocationName)
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
	if err := h.DB.UpsertSafetyAlerts(ctx, stop.ID, model.RawJSON(alertsJSON)); err != nil {
		slog.ErrorContext(ctx, "[lookout] Failed to save safety alerts", "error", err)
		return
	}

	h.broadcastProgress(sessionID, "progress", fmt.Sprintf("Safety audit complete for %s", stop.LocationName))
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

func (h *Handler) findPlannedTrackForLeg(ctx context.Context, stop *model.Stop, next *model.Stop, stopPosition int) *model.VoyageTrack {
	if stop == nil {
		return nil
	}
	tracks, err := h.DB.ListVoyageTracks(ctx, stop.VoyageID)
	if err != nil {
		tracks = nil
	}

	var plannedTracks []model.VoyageTrack
	for _, tr := range tracks {
		if tr.Kind == string(model.TrackKindPlanned) {
			plannedTracks = append(plannedTracks, tr)
		}
	}

	if len(plannedTracks) == 0 {
		if stopTracks, err := h.DB.ListStopTracks(ctx, stop.ID); err == nil {
			for _, tr := range stopTracks {
				if tr.Kind == string(model.TrackKindPlanned) {
					plannedTracks = append(plannedTracks, tr)
				}
			}
		}
		if next != nil {
			if nextTracks, err := h.DB.ListStopTracks(ctx, next.ID); err == nil {
				for _, tr := range nextTracks {
					if tr.Kind == string(model.TrackKindPlanned) {
						plannedTracks = append(plannedTracks, tr)
					}
				}
			}
		}
	}

	if len(plannedTracks) == 0 {
		return nil
	}

	// 1. Direct match on departing stop ID
	for i := range plannedTracks {
		if plannedTracks[i].VoyageStopID != nil && *plannedTracks[i].VoyageStopID == stop.ID {
			return &plannedTracks[i]
		}
	}

	// 2. Match on destination stop ID
	if next != nil {
		for i := range plannedTracks {
			if plannedTracks[i].VoyageStopID != nil && *plannedTracks[i].VoyageStopID == next.ID {
				return &plannedTracks[i]
			}
		}
	}

	// 3. Match on Leg number in track name (e.g. "Leg 1")
	for i := range plannedTracks {
		if legNum := extractLegNumber(plannedTracks[i].Name); legNum == stopPosition {
			return &plannedTracks[i]
		}
	}

	// 4. Match on start & end endpoint proximity (within 3 NM)
	if next != nil && (stop.Latitude != 0 || stop.Longitude != 0) && (next.Latitude != 0 || next.Longitude != 0) {
		for i := range plannedTracks {
			startLat, startLng, endLat, endLng, ok := extractTrackEndpoints(&plannedTracks[i])
			if ok {
				dStart := lookoutHaversineNM(startLat, startLng, stop.Latitude, stop.Longitude)
				dEnd := lookoutHaversineNM(endLat, endLng, next.Latitude, next.Longitude)
				if dStart <= 3.0 && dEnd <= 3.0 {
					return &plannedTracks[i]
				}
			}
		}
	}

	// 5. If exactly 1 planned track and 1 transit leg
	if len(plannedTracks) == 1 && stopPosition == 1 {
		return &plannedTracks[0]
	}

	// 6. Sequential index fallback
	legIdx := stopPosition - 1
	if legIdx >= 0 && legIdx < len(plannedTracks) {
		return &plannedTracks[legIdx]
	}

	return nil
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

func buildLookoutPrompt(stop *model.Stop, next *model.Stop, briefing *model.Briefing, distNM, course float64, hasCourse bool, stopPosition, totalStops int, routeWaypoints ...string) string {
	distInfo := "none — this is the last stop, no departure planned (do not generate navigation or arrival-time alerts)"
	var travelTableInfo string
	var nextStopInfo string

	if distNM > 0 {
		courseInfo := ""
		if hasCourse {
			courseInfo = fmt.Sprintf(" at a course of %.0f°", course)
		}
		if next != nil {
			nextLoc := next.LocationName
			if next.Latitude != 0 || next.Longitude != 0 {
				nextLoc = fmt.Sprintf("%s (%.4f, %.4f)", next.LocationName, next.Latitude, next.Longitude)
			}
			nextStopInfo = fmt.Sprintf("\nNext Destination: %s", nextLoc)
			distInfo = fmt.Sprintf("%.1f nautical miles%s to %s", distNM, courseInfo, next.LocationName)
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

	locationInfo := stop.LocationName
	if stop.Latitude != 0 || stop.Longitude != 0 {
		locationInfo = fmt.Sprintf("%s (%.4f, %.4f)", stop.LocationName, stop.Latitude, stop.Longitude)
	}

	var routeWaypointsInfo string
	if len(routeWaypoints) > 0 && routeWaypoints[0] != "" {
		routeWaypointsInfo = fmt.Sprintf("\n%s\n", strings.TrimSpace(routeWaypoints[0]))
	}

	return fmt.Sprintf(`Analyze the following stop data, planned passage route (if provided), and official hydrographic publications for maritime safety concerns, hazards along the route, and local recommendations. When an intended planned route is provided, evaluate safety, tidal streams, and adverse weather against the route's specific headings and waypoints. Return a JSON array of alerts.

Location: %s
Date: %s (Note: Weather and Tide data covers 48 hours starting from this date)
Stop position: %d of %d%s
Distance to next stop: %s%s%s

Weather (48h hourly forecast):
%s

Sun Phase:
%s

Tides (48h hourly forecast):
%s

Return ONLY the JSON array of alerts. If no concerns or significant trends, return [].`,
		locationInfo,
		stop.TargetDate.Format("January 2, 2006"),
		stopPosition,
		totalStops,
		nextStopInfo,
		distInfo,
		travelTableInfo,
		routeWaypointsInfo,
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
