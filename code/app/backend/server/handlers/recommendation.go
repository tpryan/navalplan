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
	"sync"
	"time"

	appcontext "app/context"
	"app/models"
)

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

	sessionID := fmt.Sprintf("recommendation_%d_%d", voyage.ID, time.Now().Unix())

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"msg":        "Recommendation generation started",
		"voyage_id":  idStr,
		"session_id": sessionID,
	})

	// Async processing
	go h.performRecommendationGeneration(voyage, sessionID)
}

func (h *Handler) performRecommendationGeneration(v *models.Voyage, sessionID string) {
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	slog.InfoContext(ctx, fmt.Sprintf("[navigator-agent] Generating recommendations for voyage %d", v.ID))

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

	// 2. Build prompt
	if v.Latitude == nil || v.Longitude == nil {
		slog.ErrorContext(ctx, "Voyage has no coordinates, cannot generate recommendations", "voyage_id", v.ID)
		return
	}

	locInfo := ""
	if v.LocationName != nil {
		locInfo = *v.LocationName
	}

	prompt := fmt.Sprintf("Recommend 15-20 anchorages, moorings, and marinas within %d %s of %f N, %f W (%s).",
		v.SearchRadius, v.SearchRadiusUnit, *v.Latitude, *v.Longitude, locInfo)

	// 3. Run Agent (streaming)
	var parsedCount int
	fullText, err := h.Agent.RunStreaming(ctx, appName, userID, sessionID, prompt)
	if err != nil {
		slog.ErrorContext(ctx, "Agent run failed", "error", err)
		return
	}

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

	slog.InfoContext(ctx, fmt.Sprintf("Generated %d recommendations for voyage %d", parsedCount, v.ID))
}
