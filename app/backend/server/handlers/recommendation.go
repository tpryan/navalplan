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
	json.NewEncoder(w).Encode(recommendations)
}

// GenerateRecommendations triggers the navigator_agent to research the voyage area.
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

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Recommendation generation started", "voyage_id": idStr})

	// Async processing
	go h.performRecommendationGeneration(voyage)
}

func (h *Handler) performRecommendationGeneration(v *models.Voyage) {
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()

	ctx := context.Background()
	slog.InfoContext(ctx, fmt.Sprintf("[navigator-agent] Generating recommendations for voyage %d", v.ID))

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "navigator_agent"
	userID := "system"
	sessionID := fmt.Sprintf("recommendation_%d", v.ID)

	client := h.AgentClient

	// 1. Create Session
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	respSession, err := client.Post(createSessionURL, "application/json", nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create agent session", "error", err)
	} else if respSession != nil {
		respSession.Body.Close()
	}

	// 2. Run Agent
	locInfo := ""
	if v.LocationName != nil {
		locInfo = *v.LocationName
	}

	prompt := ""
	if v.Latitude != nil && v.Longitude != nil {
		prompt = fmt.Sprintf("Recommend anchorages, moorings, and marinas within %d %s of %f N, %f W (%s).",
			v.SearchRadius, v.SearchRadiusUnit, *v.Latitude, *v.Longitude, locInfo)
	} else {
		prompt = fmt.Sprintf("Recommend anchorages, moorings, and marinas within %d %s of %s.",
			v.SearchRadius, v.SearchRadiusUnit, locInfo)
	}

	reqBody := AgentRunRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
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

	var events []AgentEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		slog.ErrorContext(ctx, "Failed to decode agent response", "error", err)
		return
	}

	// Find the model response
	var responseText string
	for _, e := range events {
		if e.Content.Role == "model" && len(e.Content.Parts) > 0 {
			responseText = e.Content.Parts[0].Text
		}
	}

	if responseText == "" {
		slog.ErrorContext(ctx, "No response from agent")
		return
	}

	responseText = cleanJSON(responseText)
	responseText = repairMathInJSON(responseText)

	var recommendations []models.VoyageRecommendation
	if err := json.Unmarshal([]byte(responseText), &recommendations); err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal agent JSON output", "error", err, "raw", responseText)
		return
	}

	// Filter by radius
	if v.Latitude != nil && v.Longitude != nil {
		var filtered []models.VoyageRecommendation
		for _, rec := range recommendations {
			dist := haversine(*v.Latitude, *v.Longitude, rec.Latitude, rec.Longitude, v.SearchRadiusUnit)
			if dist <= float64(v.SearchRadius) {
				filtered = append(filtered, rec)
			} else {
				slog.WarnContext(ctx, "Filtering recommendation out of range", "name", rec.Name, "dist", dist, "radius", v.SearchRadius, "unit", v.SearchRadiusUnit)
			}
		}
		recommendations = filtered
	}

	// Clear old recommendations
	if err := h.DB.DeleteVoyageRecommendations(ctx, v.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete old recommendations", "voyage_id", v.ID, "err", err)
	}

	// Save new recommendations
	for i := range recommendations {
		recommendations[i].VoyageID = v.ID
		if err := h.DB.CreateVoyageRecommendation(ctx, &recommendations[i]); err != nil {
			slog.ErrorContext(ctx, "Failed to save recommendation", "voyage_id", v.ID, "err", err)
		}
	}

	slog.InfoContext(ctx, fmt.Sprintf("Generated %d recommendations for voyage %d", len(recommendations), v.ID))
}
