package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"app/models"

	"github.com/charmbracelet/log"
	"google.golang.org/api/idtoken"

	"github.com/go-chi/chi/v5"
)

type AgentRunRequest struct {
	AppName    string `json:"appName"`
	UserID     string `json:"userId"`
	SessionID  string `json:"sessionId"`
	NewMessage struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"newMessage"`
}

type AgentEvent struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
		Role string `json:"role"`
	} `json:"content"`
}

type AgentOutput struct {
	LocationName   string          `json:"location_name"`
	WeatherSummary json.RawMessage `json:"weather_summary"`
	SunPhase       json.RawMessage `json:"sun_phase"`
	Tides          json.RawMessage `json:"tides"`
	Facilities     json.RawMessage `json:"facilities"`
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func (h *Handler) TriggerResearch(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Stop ID", http.StatusBadRequest)
		return
	}

	stop, err := h.DB.GetStop(stopID)
	if err != nil {
		http.Error(w, "Stop not found", http.StatusNotFound)
		return
	}

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "stop_id": idStr})

	// Async processing
	go h.performStopResearch(stop)
}

func (h *Handler) performStopResearch(stop *models.Stop) {
	log.SetPrefix("researcher-agent")

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "researcher_agent"
	userID := "system"
	sessionID := fmt.Sprintf("stop_%d", stop.ID)

	ctx := context.Background()

	// SECURE CLIENT CREATION
	// If we are calling a Cloud Run service securely, we need an ID Token.
	var client *http.Client
	var err error

	if strings.Contains(agentURL, "run.app") {
		// Create an authenticated client that appends the OIDC token for the specific audience (agentURL)
		client, err = idtoken.NewClient(ctx, agentURL)
		if err != nil {
			log.Errorf("Failed to create authenticated client: %v", err)
			return
		}
	} else {
		// Default client for localhost development
		client = http.DefaultClient
	}

	// 1. Create Session
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	respSession, err := client.Post(createSessionURL, "application/json", nil)
	if err != nil {
		log.Infof("Failed to create agent session: %v", err)
	} else {
		respSession.Body.Close()
	}

	// 2. Run Agent
	prompt := fmt.Sprintf("Research anchorages and weather for %f N, %f W (%s) for %s. Radius %d %s.",
		stop.Latitude, stop.Longitude, stop.LocationName, stop.TargetDate.Format("January 2, 2006"), stop.SearchRadius, stop.SearchRadiusUnit)

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
		log.Infof("Failed to call agent: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Infof("Agent returned error: %s", body)
		return
	}

	var events []AgentEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		log.Infof("Failed to decode agent response: %v", err)
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
		log.Infof("No response from agent")
		return
	}

	responseText = cleanJSON(responseText)

	var output AgentOutput
	if err := json.Unmarshal([]byte(responseText), &output); err != nil {
		log.Infof("Failed to unmarshal agent JSON output: %v. Raw: %s", err, responseText)
		return
	}

	briefing := &models.Briefing{
		StopID:         stop.ID,
		WeatherSummary: models.RawJSON(output.WeatherSummary),
		SunPhase:       models.RawJSON(output.SunPhase),
		Tides:          models.RawJSON(output.Tides),
		Facilities:     models.RawJSON(output.Facilities),
	}

	if err := h.DB.CreateBriefing(briefing); err != nil {
		log.Infof("Failed to save briefing: %v", err)
	}
	log.Infof("Briefing saved for stop %d", stop.ID)
}

func (h *Handler) GetBriefing(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Stop ID", http.StatusBadRequest)
		return
	}

	briefing, err := h.DB.GetBriefing(stopID)
	if err != nil {
		http.Error(w, "Briefing not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefing)
}

func (h *Handler) TriggerFullVoyageResearch(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	voyage, err := h.DB.GetVoyage(voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}

	stops, err := h.DB.ListStops(voyageID)
	if err != nil {
		http.Error(w, "Failed to list stops", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"msg":        "Full research started",
		"voyage_id":  voyageID,
		"stop_count": len(stops),
	})

	go func() {
		// logger := log.New(os.Stderr)
		log.SetPrefix("research-coordinator")

		// 1. Research Voyage Guide
		log.Infof("Starting guide research for voyage %d", voyageID)
		h.performGuideResearch(voyage)

		// 2. Research each stop
		for _, stop := range stops {
			// We can throttle this if needed, but for now let's just launch them
			// Maybe a small delay to not overwhelm the agent service if it's rate limited
			log.Infof("Starting stop research for stop %d", stop.ID)
			h.performStopResearch(&stop)
			time.Sleep(500 * time.Millisecond)
		}
	}()
}
