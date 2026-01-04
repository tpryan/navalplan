package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	appcontext "app/context"
	"app/models"

	"github.com/charmbracelet/log"
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

func (h *Handler) TriggerResearch(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Stop ID", http.StatusBadRequest)
		return
	}

	stop, err := h.DB.GetStop(r.Context(), stopID)
	if err != nil {
		http.Error(w, "Stop not found", http.StatusNotFound)
		return
	}

	// Check ownership via Voyage
	voyage, err := h.DB.GetVoyage(r.Context(), stop.VoyageID)
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
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "stop_id": idStr})

	// Async processing
	go h.performStopResearch(stop)
}

func (h *Handler) performStopResearch(stop *models.Stop) {
	h.ResearchSem <- struct{}{}
	defer func() { <-h.ResearchSem }()
	h.performStopResearchLogic(stop)
}

func (h *Handler) performStopResearchLogic(stop *models.Stop) {
	log.SetPrefix("researcher-agent")

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "researcher_agent"
	userID := "system"
	sessionID := fmt.Sprintf("stop_%d", stop.ID)

	ctx := context.Background()
	client := h.AgentClient

	// 1. Create Session
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	respSession, err := client.Post(createSessionURL, "application/json", nil)
	if err != nil {
		log.Infof("Failed to create agent session: %v", err)
	} else if respSession != nil {
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

	if err := h.DB.CreateBriefing(ctx, briefing); err != nil {
		log.Infof("Failed to save briefing: %v", err)
	}
	log.Infof("Briefing saved for stop %d", stop.ID)
}

func (h *Handler) GetBriefing(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Stop ID", http.StatusBadRequest)
		return
	}

	briefing, err := h.DB.GetBriefing(r.Context(), stopID)
	if err != nil {
		http.Error(w, "Briefing not found", http.StatusNotFound)
		return
	}

	// Check ownership via Stop -> Voyage
	stop, err := h.DB.GetStop(r.Context(), briefing.StopID)
	if err != nil {
		http.Error(w, "Stop not found", http.StatusInternalServerError)
		return
	}
	voyage, err := h.DB.GetVoyage(r.Context(), stop.VoyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusInternalServerError)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefing)
}

func (h *Handler) TriggerFullVoyageResearch(w http.ResponseWriter, r *http.Request) {
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

	stops, err := h.DB.ListStops(r.Context(), voyageID, 0, 0)
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
		log.SetPrefix("research-coordinator")

		var wg sync.WaitGroup

		// 1. Research Voyage Guide (Parallel)
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Infof("Starting guide research for voyage %d", voyageID)
			h.performGuideResearch(voyage)
		}()

		// 2. Research each stop (Parallel)
		for _, stop := range stops {
			wg.Add(1)
			h.ResearchSem <- struct{}{} // Block until a slot is available
			go func(s models.Stop) {
				defer wg.Done()
				defer func() { <-h.ResearchSem }() // Release slot
				log.Infof("Starting stop research for stop %d", s.ID)
				h.performStopResearchLogic(&s)
			}(stop)
		}

		wg.Wait()
		log.Infof("Full research complete for voyage %d", voyageID)
	}()
}
