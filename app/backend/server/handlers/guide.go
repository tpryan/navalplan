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
	"time"

	"app/models"

	"github.com/go-chi/chi/v5"
)

type GuideAgentOutput struct {
	Summary       string          `json:"summary"`
	SailingSeason json.RawMessage `json:"sailing_season"`
	Hazards       json.RawMessage `json:"hazards"`
	Hubs          json.RawMessage `json:"hubs"`
	CharterInfo   json.RawMessage `json:"charter_info"`
}

func (h *Handler) TriggerGuideResearch(w http.ResponseWriter, r *http.Request) {
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

	// Respond immediately
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "voyage_id": idStr})

	// Async processing
	go func() {
		agentURL := os.Getenv("NAVALPLAN_AGENT_URL")
		if agentURL == "" {
			agentURL = "http://127.0.0.1:8081"
		}

		appName := "guide_agent"
		userID := "system"
		sessionID := fmt.Sprintf("voyage_%d", voyage.ID)

		// 1. Create Session
		createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
		respSession, err := http.Post(createSessionURL, "application/json", nil)
		if err != nil {
			log.Printf("Failed to create agent session: %v", err)
		} else {
			respSession.Body.Close()
		}

		// 2. Run Agent
		locName := "Unknown"
		if voyage.LocationName != nil {
			locName = *voyage.LocationName
		}

		prompt := fmt.Sprintf("Research sailing guide for location: %s", locName)

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

		var output GuideAgentOutput
		if err := json.Unmarshal([]byte(responseText), &output); err != nil {
			log.Printf("Failed to unmarshal agent JSON output: %v. Raw: %s", err, responseText)
			return
		}

		guide := &models.VoyageGuide{
			VoyageID:      voyage.ID,
			Summary:       output.Summary,
			SailingSeason: models.RawJSON(output.SailingSeason),
			Hazards:       models.RawJSON(output.Hazards),
			Hubs:          models.RawJSON(output.Hubs),
			CharterInfo:   models.RawJSON(output.CharterInfo),
			CreatedAt:     time.Now(),
		}

		if err := h.DB.CreateVoyageGuide(guide); err != nil {
			log.Printf("Failed to save voyage guide: %v", err)
		}
		log.Printf("Voyage guide saved for voyage %d", voyage.ID)
	}()
}

func (h *Handler) GetVoyageGuide(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	guide, err := h.DB.GetVoyageGuide(voyageID)
	if err != nil {
		http.Error(w, "Voyage guide not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(guide)
}
