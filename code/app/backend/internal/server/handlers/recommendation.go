package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"app/internal/model"
)

const (
	// recGeocodeSnapMinNM is the minimum discrepancy (NM) between an LLM-emitted
	// anchorage/mooring coordinate and Google Geocoding's location for that named
	// feature before we snap to the geocoded position (catches ~2-8 NM LLM memory drift
	// such as placing Bloody Point Anchorage on Poplar Island).
	recGeocodeSnapMinNM = 0.75
	// recGeocodeSnapMaxNM caps how far a geocode match may be from the LLM coordinate
	// so an unrelated distant namesake cannot hijack the pin.
	recGeocodeSnapMaxNM = 12.0
)

var genericMarineSuffixes = []string{
	" Anchorage", " Mooring Field", " Moorings", " Mooring",
	" Harbor Hub", " Boating Hub", " Central Hub", " Hub",
}

func cleanGeocodeFeatureName(name string) string {
	cleaned := strings.TrimSpace(name)
	lower := strings.ToLower(cleaned)
	for _, suffix := range genericMarineSuffixes {
		if strings.HasSuffix(lower, strings.ToLower(suffix)) && len(cleaned) > len(suffix)+2 {
			cleaned = strings.TrimSpace(cleaned[:len(cleaned)-len(suffix)])
			break
		}
	}
	return cleaned
}

func refineRecommendationCoordinates(ctx context.Context, recs []model.VoyageRecommendation, vicinity string, centerLat, centerLng, maxRadiusNM float64) {
	if len(recs) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range recs {
		recType := strings.ToLower(strings.TrimSpace(recs[i].Type))
		needsCheck := (recs[i].Latitude == 0 && recs[i].Longitude == 0) ||
			strings.Contains(recType, "anchor") ||
			strings.Contains(recType, "moor") ||
			recType == "hub"
		if !needsCheck {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := &recs[idx]
			queryName := cleanGeocodeFeatureName(r.Name)
			lat, lng, err := geocodeFacility(ctx, queryName, vicinity, centerLat, centerLng)
			if err != nil || (lat == 0 && lng == 0) {
				// If the LLM emitted (0,0) and full-name geocoding failed, retry with the
				// raw recommendation name before giving up.
				if r.Latitude == 0 && r.Longitude == 0 && queryName != r.Name {
					lat, lng, err = geocodeFacility(ctx, r.Name, vicinity, centerLat, centerLng)
				}
				if err != nil || (lat == 0 && lng == 0) {
					return
				}
			}
			if haversine(centerLat, centerLng, lat, lng, "nm") > maxRadiusNM {
				return
			}
			if r.Latitude == 0 && r.Longitude == 0 {
				slog.InfoContext(ctx, "Geocoded recommendation missing coordinates",
					"name", r.Name, "query", queryName, "lat", lat, "lng", lng)
				r.Latitude, r.Longitude = lat, lng
				return
			}
			driftNM := haversine(r.Latitude, r.Longitude, lat, lng, "nm")
			if driftNM >= recGeocodeSnapMinNM && driftNM <= recGeocodeSnapMaxNM {
				slog.InfoContext(ctx, "Snapped drifted recommendation coordinates to geocoded feature",
					"name", r.Name, "query", queryName,
					"old_lat", r.Latitude, "old_lng", r.Longitude,
					"new_lat", lat, "new_lng", lng, "drift_nm", driftNM)
				r.Latitude, r.Longitude = lat, lng
			}
		}(i)
	}
	wg.Wait()
}

func radiusToNM(radius int, unit string) float64 {
	r := float64(radius)
	switch unit {
	case "mi":
		return r * 0.868976
	case "km":
		return r / 1.852
	default:
		return r
	}
}

func recommendationCount(radius int, unit string) int {
	nm := radiusToNM(radius, unit)
	count := int(math.Round(nm * 3.0))
	if count < 25 {
		count = 25
	}
	if count > 60 {
		count = 60
	}
	return count
}

func searchBoundaryHint(centerLat, centerLng float64, radiusNM float64) string {
	latDeg := radiusNM / 60.0
	lngDeg := radiusNM / (60.0 * math.Cos(centerLat*math.Pi/180.0))

	nLat := centerLat + latDeg
	sLat := centerLat - latDeg
	eLng := centerLng + lngDeg
	wLng := centerLng - lngDeg

	fmtLat := func(lat float64) string {
		if lat >= 0 {
			return fmt.Sprintf("%.2f°N", lat)
		}
		return fmt.Sprintf("%.2f°S", math.Abs(lat))
	}

	fmtLng := func(lng float64) string {
		if lng >= 0 {
			return fmt.Sprintf("%.2f°E", lng)
		}
		return fmt.Sprintf("%.2f°W", math.Abs(lng))
	}

	hint := fmt.Sprintf(
		"The circle boundary reaches approximately: North edge (%s, %s), South edge (%s, %s), East edge (%s, %s), West edge (%s, %s). "+
			"When the center is in open water (e.g. a strait, sound, or bay), identify the coastlines, cities, islands, and inlets located near EACH of those four perimeter coordinates (especially both the West edge and East edge, or North edge and South edge) and include their marinas, moorings, fuel docks, and anchorages — never cover only one side of an open-water crossing!",
		fmtLat(nLat), fmtLng(centerLng),
		fmtLat(sLat), fmtLng(centerLng),
		fmtLat(centerLat), fmtLng(eLng),
		fmtLat(centerLat), fmtLng(wLng),
	)
	if radiusNM > 27.0 {
		hint += " For this large search area (>27 NM), execute your parallel searches in Turn 1 across both central and outer compass sectors."
	}
	return hint
}

func haversine(lat1, lon1, lat2, lon2 float64, unit string) float64 {
	const (
		earthRadiusKm = 6371.0
		earthRadiusMi = 3958.8
		earthRadiusNm = 3440.1
	)

	var r float64
	switch unit {
	case "nm":
		r = earthRadiusNm
	case "mi":
		r = earthRadiusMi
	case "km":
		r = earthRadiusKm
	default:
		r = earthRadiusNm
	}

	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	deltaPhi := (lat2 - lat1) * math.Pi / 180
	deltaLambda := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*
			math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return r * c
}

func (h *Handler) ListRecommendations(w http.ResponseWriter, r *http.Request) {
	v, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := v.ID

	recommendations, err := h.DB.ListVoyageRecommendations(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list recommendations", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"recommendations": recommendations,
	})
}

func (h *Handler) ensureRecChannel(sessionID string, ttl time.Duration) chan model.VoyageRecommendation {
	return h.recHub.ensure(sessionID, ttl)
}

func (h *Handler) broadcastRecommendation(sessionID string, rec model.VoyageRecommendation) {
	h.recHub.broadcast(sessionID, rec)
}

func (h *Handler) StreamRecommendations(w http.ResponseWriter, r *http.Request) {
	streamSSE(w, r, &h.recHub, "recommendation", 30*time.Minute, nil)
}

func (h *Handler) GenerateRecommendations(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	idStr := r.PathValue("id")

	ts := time.Now().Unix()
	sessionID := fmt.Sprintf("recommendation_%d_%d", voyage.ID, ts)
	progressSessionID := fmt.Sprintf("rec_progress_%d_%d", voyage.ID, ts)

	h.ensureProgressChannel(progressSessionID, 25*time.Minute)
	h.ensureRecChannel(sessionID, 25*time.Minute)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"msg":                 "Recommendation generation started",
		"voyage_id":           idStr,
		"session_id":          sessionID,
		"progress_session_id": progressSessionID,
	})

	go h.performRecommendationGeneration(voyage, sessionID, progressSessionID)
}

func (h *Handler) performRecommendationGeneration(v *model.Voyage, sessionID, progressSessionID string) {
	h.ResearchSem <- struct{}{}
	defer func() {
		<-h.ResearchSem
		h.recHub.close(sessionID)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, "[navigator-agent] Generating recommendations for voyage", "voyage_id", v.ID)
	h.broadcastProgress(progressSessionID, "start", "Starting Local Pilot research")

	const appName = "specialist"
	const userID = "system"

	if err := h.DB.DeleteVoyageRecommendations(ctx, v.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old recommendations", "voyage_id", v.ID, "error", err)
	}

	radiusNM := radiusToNM(v.SearchRadius, v.SearchRadiusUnit)
	sessionState := map[string]any{
		"search_mode":        "area",
		"area_radius_meters": radiusNM * 1852.0,
	}
	if err := h.Agent.CreateSession(ctx, appName, userID, sessionID, sessionState); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		h.broadcastProgress(progressSessionID, "error", "Failed to start research session")
		return
	}

	h.broadcastProgress(progressSessionID, "agent", "Consulting the Local Pilot specialist agent")

	if v.Latitude == nil || v.Longitude == nil {
		slog.ErrorContext(ctx, "Voyage has no coordinates, cannot generate recommendations", "voyage_id", v.ID)
		h.broadcastProgress(progressSessionID, "error", "Voyage has no location set — please add coordinates first")
		return
	}

	locInfo := ""
	if v.LocationName != nil {
		locInfo = *v.LocationName
	}

	count := recommendationCount(v.SearchRadius, v.SearchRadiusUnit)
	boundaryHint := searchBoundaryHint(*v.Latitude, *v.Longitude, radiusNM)

	var latStr string
	if *v.Latitude >= 0 {
		latStr = fmt.Sprintf("%.4f°N", *v.Latitude)
	} else {
		latStr = fmt.Sprintf("%.4f°S", math.Abs(*v.Latitude))
	}
	var lngStr string
	if *v.Longitude >= 0 {
		lngStr = fmt.Sprintf("%.4f°E", *v.Longitude)
	} else {
		lngStr = fmt.Sprintf("%.4f°W", math.Abs(*v.Longitude))
	}

	prompt := fmt.Sprintf(
		"Recommend anchorages, moorings, marinas, and fuel docks within %d %s of center coordinates (latitude: %.4f, longitude: %.4f, %s, %s; center reference point near %s). "+
			"IMPORTANT: %q is only the nearest point to the circle center — the search circle spans %d %s in ALL directions (N, NE, E, SE, S, SW, W, NW) across multiple shores, harbors, and rivers. "+
			"Your batch_google_search queries and recommendations MUST explicitly cover major harbors, rivers, and creeks on EVERY shore inside the boundary (including Western/NW/SW shores as well as Eastern/NE/SE shores), not just %s. "+
			"Return at least %d recommendations, and include EVERY qualifying marina, mooring field, fuel dock, yacht club, and anchorage you find with verified coordinates — there is no upper limit. "+
			"Pass the exact center coordinates (latitude: %.4f, longitude: %.4f) and full radius (%d %s = %.0f meters) to facility_discovery with mode: \"area\" — do NOT shift the center to a single island or town. %s",
		v.SearchRadius, v.SearchRadiusUnit, *v.Latitude, *v.Longitude, latStr, lngStr, locInfo,
		locInfo, v.SearchRadius, v.SearchRadiusUnit, locInfo,
		count, *v.Latitude, *v.Longitude, v.SearchRadius, v.SearchRadiusUnit, radiusNM*1852.0, boundaryHint)

	if v.StartDate != nil && v.EndDate != nil && v.StartDate.Equal(*v.EndDate) {
		prompt += " This is a day trip — a single-day outing with no overnight stay. " +
			"Prioritize day-use anchorages, lunch stops, and moorings suited to a few hours rather than overnight-only spots."
	}

	var parsedCount int
	progressStop := make(chan struct{})
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		messages := []string{
			"Consulting regional nautical charts and pilot guides",
			"Evaluating anchorages and mooring fields across sectors",
			"Assessing shelter, depth, and nautical facilities",
			"Synthesizing recommendations for navigational review",
		}
		msgIdx := 0
		for {
			select {
			case <-progressStop:
				return
			case <-t.C:
				msg := messages[msgIdx%len(messages)]
				msgIdx++
				h.broadcastProgress(progressSessionID, "agent", msg)
			}
		}
	}()
	fullText, err := h.Agent.RunStreaming(ctx, appName, userID, sessionID, prompt)
	close(progressStop)
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
		if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "high demand") {
			h.broadcastProgress(progressSessionID, "error_503", "Model is busy due to high demand. Please try again in a few minutes.")
		} else if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") || strings.Contains(err.Error(), "Resource exhausted") {
			h.broadcastProgress(progressSessionID, "error_429", "Model rate limit exceeded. Please wait a moment and try again.")
		} else {
			h.broadcastProgress(progressSessionID, "error", "Research agent failed to respond")
		}
		return
	}

	if strings.TrimSpace(fullText) == "" {
		slog.ErrorContext(ctx, "Agent returned empty response")
		h.broadcastProgress(progressSessionID, "error", "Research agent returned an empty response — please try again")
		return
	}

	h.broadcastProgress(progressSessionID, "parsing", "Charting anchorages, moorings, and marinas")

	fullText = cleanJSON(fullText)
	fullText = repairMathInJSON(fullText)

	var wrapper struct {
		Recommendations []model.VoyageRecommendation `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(fullText), &wrapper); err != nil {
		var directRecs []model.VoyageRecommendation
		if errArray := json.Unmarshal([]byte(fullText), &directRecs); errArray == nil {
			wrapper.Recommendations = directRecs
		} else {
			repaired := repairTruncatedJSONArray(fullText)
			if errRepaired := json.Unmarshal([]byte(repaired), &wrapper); errRepaired == nil && len(wrapper.Recommendations) > 0 {
				slog.WarnContext(ctx, "Salvaged recommendations from truncated agent response", "count", len(wrapper.Recommendations))
			} else if errDirectRepaired := json.Unmarshal([]byte(repaired), &directRecs); errDirectRepaired == nil && len(directRecs) > 0 {
				wrapper.Recommendations = directRecs
				slog.WarnContext(ctx, "Salvaged recommendations from truncated agent array", "count", len(wrapper.Recommendations))
			} else {
				slog.ErrorContext(ctx, "Failed to unmarshal agent JSON output", "error", err, "raw", fullText)
				h.broadcastProgress(progressSessionID, "error", "Agent returned an unreadable response — please try again")
				return
			}
		}
	}

	recommendations := wrapper.Recommendations
	refineRecommendationCoordinates(ctx, recommendations, locInfo, *v.Latitude, *v.Longitude, radiusNM)

	for _, rec := range recommendations {
		if rec.Latitude == 0 && rec.Longitude == 0 {
			continue
		}
		dist := haversine(*v.Latitude, *v.Longitude, rec.Latitude, rec.Longitude, v.SearchRadiusUnit)
		if dist > float64(v.SearchRadius) {
			continue
		}
		rec.RadiusMiles = haversine(*v.Latitude, *v.Longitude, rec.Latitude, rec.Longitude, "mi")

		rec.VoyageID = v.ID
		if err := h.DB.CreateVoyageRecommendation(ctx, &rec); err != nil {
			slog.ErrorContext(ctx, "Failed to save recommendation", "voyage_id", v.ID, "error", err)
			continue
		}

		h.broadcastRecommendation(sessionID, rec)
		parsedCount++
	}

	h.broadcastProgress(progressSessionID, "done", fmt.Sprintf("Local Pilot complete — %d locations charted", parsedCount))
	slog.InfoContext(ctx, "Generated recommendations for voyage", "count", parsedCount, "voyage_id", v.ID)
}
