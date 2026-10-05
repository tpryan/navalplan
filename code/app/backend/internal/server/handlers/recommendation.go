package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"app/internal/model"
)

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
	count := int(math.Round(nm * 1.0))
	if count < 15 {
		count = 15
	}
	if count > 30 {
		count = 30
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

	return fmt.Sprintf(
		"The circle boundary reaches approximately: N %s, S %s, E %s, W %s. "+
			"Make sure to include sailing spots near ALL four edges of this boundary, "+
			"not only near the center or the most prominent harbour.",
		fmtLat(nLat), fmtLat(sLat), fmtLng(eLng), fmtLng(wLng),
	)
}

func repairMathInJSON(s string) string {
	re := regexp.MustCompile(`(-?\d+\.?\d*)\s*([+-])\s*(\d+\.?\d*)`)

	return re.ReplaceAllStringFunc(s, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		v1, _ := strconv.ParseFloat(parts[1], 64)
		op := parts[2]
		v2, _ := strconv.ParseFloat(parts[3], 64)

		var res float64
		if op == "+" {
			res = v1 + v2
		} else {
			res = v1 - v2
		}

		return fmt.Sprintf("%.7f", res)
	})
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

	v, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	recommendations, err := h.DB.ListVoyageRecommendations(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list recommendations", "voyage_id", voyageID, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"recommendations": recommendations,
	})
}

func (h *Handler) broadcastRecommendation(sessionID string, rec model.VoyageRecommendation) {
	h.muRecStreams.RLock()
	defer h.muRecStreams.RUnlock()
	if ch, ok := h.recStreams[sessionID]; ok {
		select {
		case ch <- rec:
		default:
		}
	}
}

func (h *Handler) StreamRecommendations(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "Missing session_id")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	rc := http.NewResponseController(w)

	ch := make(chan model.VoyageRecommendation, 10)
	var once sync.Once
	closeCh := func() { once.Do(func() { close(ch) }) }

	h.muRecStreams.Lock()
	h.recStreams[sessionID] = ch
	h.muRecStreams.Unlock()

	defer func() {
		h.muRecStreams.Lock()
		delete(h.recStreams, sessionID)
		closeCh()
		h.muRecStreams.Unlock()
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			rc.Flush()
		case rec, ok := <-ch:
			if !ok {
				return
			}
			jsonData, _ := json.Marshal(rec)
			fmt.Fprintf(w, "event: recommendation\ndata: %s\n\n", jsonData)
			rc.Flush()
		}
	}
}

func (h *Handler) GenerateRecommendations(w http.ResponseWriter, r *http.Request) {
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

	ts := time.Now().Unix()
	sessionID := fmt.Sprintf("recommendation_%d_%d", voyage.ID, ts)
	progressSessionID := fmt.Sprintf("rec_progress_%d_%d", voyage.ID, ts)

	h.ensureProgressChannel(progressSessionID, 25*time.Minute)

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
		h.muRecStreams.Lock()
		if ch, ok := h.recStreams[sessionID]; ok {
			close(ch)
			delete(h.recStreams, sessionID)
		}
		h.muRecStreams.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, fmt.Sprintf("[navigator-agent] Generating recommendations for voyage %d", v.ID))
	h.broadcastProgress(progressSessionID, "start", "Starting Local Pilot research")

	const appName = "specialist"
	const userID = "system"

	if err := h.DB.DeleteVoyageRecommendations(ctx, v.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old recommendations", "voyage_id", v.ID, "err", err)
	}

	if err := h.Agent.CreateSession(ctx, appName, userID, sessionID, nil); err != nil {
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
	radiusNM := radiusToNM(v.SearchRadius, v.SearchRadiusUnit)
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
		"Recommend %d anchorages, moorings, and marinas within %d %s of coordinates (latitude: %.4f, longitude: %.4f, approximately %s, %s, %s). %s",
		count, v.SearchRadius, v.SearchRadiusUnit, *v.Latitude, *v.Longitude, latStr, lngStr, locInfo,
		boundaryHint)

	if v.StartDate != nil && v.EndDate != nil && v.StartDate.Equal(*v.EndDate) {
		prompt += " This is a day trip — a single-day outing with no overnight stay. " +
			"Prioritize day-use anchorages, lunch stops, and moorings suited to a few hours rather than overnight-only spots."
	}

	var parsedCount int
	fullText, err := h.Agent.RunStreaming(ctx, appName, userID, sessionID, prompt)
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

	for _, rec := range recommendations {
		if v.Latitude != nil && v.Longitude != nil {
			dist := haversine(*v.Latitude, *v.Longitude, rec.Latitude, rec.Longitude, v.SearchRadiusUnit)
			if dist > float64(v.SearchRadius) {
				continue
			}
			rec.RadiusMiles = haversine(*v.Latitude, *v.Longitude, rec.Latitude, rec.Longitude, "mi")
		}

		rec.VoyageID = v.ID
		if err := h.DB.CreateVoyageRecommendation(ctx, &rec); err != nil {
			slog.ErrorContext(ctx, "Failed to save recommendation", "voyage_id", v.ID, "err", err)
			continue
		}

		h.broadcastRecommendation(sessionID, rec)
		parsedCount++
	}

	h.broadcastProgress(progressSessionID, "done", fmt.Sprintf("Local Pilot complete — %d locations charted", parsedCount))
	slog.InfoContext(ctx, fmt.Sprintf("Generated %d recommendations for voyage %d", parsedCount, v.ID))
}
