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

	appcontext "app/context"
	"app/models"
)

// radiusToNM converts the voyage search radius to nautical miles.
func radiusToNM(radius int, unit string) float64 {
	r := float64(radius)
	switch unit {
	case "mi":
		return r * 0.868976
	case "km":
		return r / 1.852
	default: // "nm"
		return r
	}
}

// recommendationCount scales the number of requested recommendations to the search
// radius so that larger areas get proportionally more results.
// Formula: clamp(radius_nm * 2, 20, 50)
// Examples: 5nm→20, 12nm→24, 25nm→50, 50nm→50
func recommendationCount(radius int, unit string) int {
	nm := radiusToNM(radius, unit)
	count := int(math.Round(nm * 2))
	if count < 20 {
		count = 20
	}
	if count > 50 {
		count = 50
	}
	return count
}

// searchBoundaryHint computes the four cardinal boundary coordinates of the search
// circle and returns a sentence the model can use to understand the full geographic
// extent of the search area.
func searchBoundaryHint(centerLat, centerLng float64, radiusNM float64) string {
	// 1 nm = 1/60 degree latitude (constant)
	latDeg := radiusNM / 60.0
	// longitude degrees per nm shrinks toward the poles
	lngDeg := radiusNM / (60.0 * math.Cos(centerLat*math.Pi/180.0))

	nLat := centerLat + latDeg
	sLat := centerLat - latDeg
	// centerLng is negative for West; adding lngDeg moves East (less negative)
	eLng := centerLng + lngDeg
	wLng := centerLng - lngDeg

	fmtLng := func(lng float64) string {
		if lng <= 0 {
			return fmt.Sprintf("%.2f°W", math.Abs(lng))
		}
		return fmt.Sprintf("%.2f°E", lng)
	}

	return fmt.Sprintf(
		"The circle boundary reaches approximately: N %.2f°N, S %.2f°N, E %s, W %s. "+
			"Make sure to include sailing spots near ALL four edges of this boundary, "+
			"not only near the center or the most prominent harbour.",
		nLat, sLat, fmtLng(eLng), fmtLng(wLng),
	)
}

// repairMathInJSON looks for common math expressions like "38.97 - 0.02" in JSON
// and replaces them with the calculated result.
func repairMathInJSON(s string) string {
	// Pattern for "number space [+ or -] space number"
	// This is a common failure mode for LLMs in coordinate calculations.
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

// ListRecommendations returns generated recommendations for a voyage.
func (h *Handler) ListRecommendations(w http.ResponseWriter, r *http.Request) {
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

	// Check ownership
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

func (h *Handler) broadcastRecommendation(sessionID string, rec models.VoyageRecommendation) {
	h.muRecStreams.RLock()
	defer h.muRecStreams.RUnlock()
	if ch, ok := h.recStreams[sessionID]; ok {
		select {
		case ch <- rec:
		default:
		}
	}
}

// StreamRecommendations provides an SSE endpoint for real-time recommendation updates.
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

	ch := make(chan models.VoyageRecommendation, 10)
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

	for {
		select {
		case <-r.Context().Done():
			return
		case rec := <-ch:
			jsonData, _ := json.Marshal(rec)
			fmt.Fprintf(w, "event: recommendation\ndata: %s\n\n", jsonData)
			rc.Flush()
		}
	}
}

// GenerateRecommendations triggers the specialist agent to research the voyage area.
func (h *Handler) GenerateRecommendations(w http.ResponseWriter, r *http.Request) {
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

	ts := time.Now().Unix()
	sessionID := fmt.Sprintf("recommendation_%d_%d", voyage.ID, ts)
	progressSessionID := fmt.Sprintf("rec_progress_%d_%d", voyage.ID, ts)

	// Pre-register progress channel before spawning goroutine so early events are buffered.
	h.ensureProgressChannel(progressSessionID, 25*time.Minute)

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"msg":                 "Recommendation generation started",
		"voyage_id":           idStr,
		"session_id":          sessionID,
		"progress_session_id": progressSessionID,
	})

	// Async processing
	go h.performRecommendationGeneration(voyage, sessionID, progressSessionID)
}

func (h *Handler) performRecommendationGeneration(v *models.Voyage, sessionID, progressSessionID string) {
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, fmt.Sprintf("[navigator-agent] Generating recommendations for voyage %d", v.ID))
	h.broadcastProgress(progressSessionID, "start", "Starting Local Pilot research")

	const appName = "specialist"
	const userID = "system"

	// 0. Clear old recommendations
	if err := h.DB.DeleteVoyageRecommendations(ctx, v.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old recommendations", "voyage_id", v.ID, "err", err)
	}

	// 1. Create Session
	if err := h.Agent.CreateSession(ctx, appName, userID, sessionID, nil); err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		return
	}

	h.broadcastProgress(progressSessionID, "agent", "Consulting the Local Pilot specialist agent")

	// 2. Build prompt
	if v.Latitude == nil || v.Longitude == nil {
		slog.ErrorContext(ctx, "Voyage has no coordinates, cannot generate recommendations", "voyage_id", v.ID)
		return
	}

	locInfo := ""
	if v.LocationName != nil {
		locInfo = *v.LocationName
	}

	count := recommendationCount(v.SearchRadius, v.SearchRadiusUnit)
	radiusNM := radiusToNM(v.SearchRadius, v.SearchRadiusUnit)
	boundaryHint := searchBoundaryHint(*v.Latitude, *v.Longitude, radiusNM)

	prompt := fmt.Sprintf(
		"Recommend %d anchorages, moorings, and marinas within %d %s of %.4f°N, %.4f°W (%s). %s",
		count, v.SearchRadius, v.SearchRadiusUnit, *v.Latitude, math.Abs(*v.Longitude), locInfo,
		boundaryHint)

	// 3. Run Agent (streaming)
	var parsedCount int
	fullText, err := h.Agent.RunStreaming(ctx, appName, userID, sessionID, prompt)
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
		if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "high demand") {
			h.broadcastProgress(progressSessionID, "error_503", "Model is busy due to high demand. Please try again in a few minutes.")
		}
		return
	}

	h.broadcastProgress(progressSessionID, "parsing", "Charting anchorages, moorings, and marinas")

	fullText = cleanJSON(fullText)
	fullText = repairMathInJSON(fullText)

	var wrapper struct {
		Recommendations []models.VoyageRecommendation `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(fullText), &wrapper); err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal agent JSON output", "error", err, "raw", fullText)
		return
	}

	recommendations := wrapper.Recommendations

	// Process and save
	for _, rec := range recommendations {
		// Filter by radius and record distance in miles.
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
