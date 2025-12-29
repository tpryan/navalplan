package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"app/models"

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
	go func() {
		agentURL := os.Getenv("NAVALPLAN_AGENT_URL")
		if agentURL == "" {
			agentURL = "http://127.0.0.1:8081"
		}

		appName := "researcher_agent"
		userID := "system"
		sessionID := fmt.Sprintf("stop_%d", stop.ID)

		// 1. Create Session
		createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
		respSession, err := http.Post(createSessionURL, "application/json", nil)
		if err != nil {
			log.Printf("Failed to create agent session: %v", err)
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
		reqBody.NewMessage.Parts = []struct{ Text string `json:"text"` }{{Text: prompt}}

		jsonData, _ := json.Marshal(reqBody)
		resp, err := http.Post(agentURL+"/api/run", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("Failed to call agent: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Agent returned error: %s", body)
			return
		}

		var events []AgentEvent
		if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
			log.Printf("Failed to decode agent response: %v", err)
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
			log.Println("No response from agent")
			return
		}

		responseText = cleanJSON(responseText)

		var output AgentOutput
		if err := json.Unmarshal([]byte(responseText), &output); err != nil {
			log.Printf("Failed to unmarshal agent JSON output: %v. Raw: %s", err, responseText)
			return
		}

		briefing := &models.Briefing{
			StopID:         stop.ID,
			WeatherSummary: models.RawJSON(output.WeatherSummary),
			Tides:          models.RawJSON(output.Tides),
			Facilities:     models.RawJSON(output.Facilities),
		}

		if err := h.DB.CreateBriefing(briefing); err != nil {
			log.Printf("Failed to save briefing: %v", err)
		}
		log.Printf("Briefing saved for stop %d", stop.ID)
	}()
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
