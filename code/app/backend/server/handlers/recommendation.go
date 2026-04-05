package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	// Check ownership
	v, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if v.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	recommendations, err := h.DB.ListVoyageRecommendations(r.Context(), voyageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list recommendations", "voyage_id", voyageID, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"recommendations": recommendations,
	})
}

var (
	muRecStreams sync.RWMutex
	recStreams   = make(map[string]chan models.VoyageRecommendation)
)

func broadcastRecommendation(sessionID string, rec models.VoyageRecommendation) {
	muRecStreams.RLock()
	defer muRecStreams.RUnlock()
	if ch, ok := recStreams[sessionID]; ok {
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
		http.Error(w, "Missing session_id", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	rc := http.NewResponseController(w)

	ch := make(chan models.VoyageRecommendation, 10)
	muRecStreams.Lock()
	recStreams[sessionID] = ch
	muRecStreams.Unlock()

	defer func() {
		muRecStreams.Lock()
		delete(recStreams, sessionID)
		close(ch)
		muRecStreams.Unlock()
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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
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

	ctx := context.Background()
	slog.InfoContext(ctx, fmt.Sprintf("[navigator-agent] Generating recommendations for voyage %d", v.ID))

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "specialist"
	userID := "system"

	client := h.AgentClient

	// 0. Clear old recommendations
	if err := h.DB.DeleteVoyageRecommendations(ctx, v.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old recommendations", "voyage_id", v.ID, "err", err)
	}

	// 1. Create Session
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	respSession, err := client.Post(createSessionURL, "application/json", nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
		return
	}
	respSession.Body.Close()
	if respSession.StatusCode >= http.StatusInternalServerError {
		slog.ErrorContext(ctx, "Agent session creation returned server error", "status", respSession.StatusCode)
		return
	}

	// 2. Run Agent
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

	reqBody := AgentRunRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
		Stream:    true,
	}
	reqBody.NewMessage.Role = "user"
	reqBody.NewMessage.Parts = []struct {
		Text string `json:"text"`
	}{{Text: prompt}}

	jsonData, _ := json.Marshal(reqBody)
	resp, err := client.Post(agentURL+"/api/run", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to call agent", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.ErrorContext(ctx, "Agent returned error", "body", string(body))
		return
	}

	// 3. Stream and Parse Recommendations
	// Read first byte to determine format
	firstByte := make([]byte, 1)
	n, _ := resp.Body.Read(firstByte)
	
	var fullText string
	var parsedCount int

	if n > 0 && firstByte[0] == '[' {
		// It's a full array of events (likely non-streamed or final output)
		// Read the rest
		rest, _ := io.ReadAll(resp.Body)
		var events []AgentEvent
		allData := append(firstByte, rest...)
		if err := json.Unmarshal(allData, &events); err != nil {
			slog.ErrorContext(ctx, "Failed to unmarshal agent events array", "error", err)
		} else {
			for _, e := range events {
				if len(e.Content.Parts) > 0 {
					fullText += e.Content.Parts[0].Text
				}
			}
		}
	} else {
		// It's likely NDJSON or a single object (streaming mode)
		// We need to re-read the first byte from our combined reader
		multi := io.MultiReader(bytes.NewReader(firstByte), resp.Body)
		decoder := json.NewDecoder(multi)
		for {
			var event AgentEvent
			if err := decoder.Decode(&event); err == io.EOF {
				break
			} else if err != nil {
				// Try to see if it's just raw text remaining
				slog.ErrorContext(ctx, "Failed to decode agent event", "error", err)
				break
			}

			if len(event.Content.Parts) > 0 {
				text := event.Content.Parts[0].Text
				fullText += text
				// Optional: In a more advanced implementation, we could try parsing individual 
				// recommendations from fullText here for even faster broadcast.
			}
		}
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
		// Filter by radius
		if v.Latitude != nil && v.Longitude != nil {
			dist := haversine(*v.Latitude, *v.Longitude, rec.Latitude, rec.Longitude, v.SearchRadiusUnit)
			if dist > float64(v.SearchRadius) {
				continue
			}
		}

		rec.VoyageID = v.ID
		if err := h.DB.CreateVoyageRecommendation(ctx, &rec); err != nil {
			slog.ErrorContext(ctx, "Failed to save recommendation", "voyage_id", v.ID, "err", err)
			continue
		}
		
		broadcastRecommendation(sessionID, rec)
		parsedCount++
	}

	slog.InfoContext(ctx, fmt.Sprintf("Generated %d recommendations for voyage %d", parsedCount, v.ID))
}
