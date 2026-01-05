package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"app/models"

	"github.com/charmbracelet/log"
)

type DiscoveryRegionOutput struct {
	Name              string          `json:"name"`
	Type              string          `json:"type"`
	IsHiddenGem       bool            `json:"is_hidden_gem"`
	SuitabilityScore  int             `json:"suitability_score"`
	Summary           string          `json:"summary"`
	DeepCutReasoning  string          `json:"deep_cut_reasoning"`
	AvgWindSpeedKnots int             `json:"avg_wind_speed_knots"`
	AvgTempC          int             `json:"avg_temp_c"`
	Geometry          json.RawMessage `json:"geometry"`
}

func (h *Handler) GetDiscoveryRegions(w http.ResponseWriter, r *http.Request) {
	monthStr := r.URL.Query().Get("month")
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		// Default to current month if missing or invalid
		month = int(time.Now().Month())
	}

	regions, err := h.DB.ListRegionsByMonth(r.Context(), month)
	if err != nil {
		http.Error(w, "Failed to list regions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(regions)
}

func (h *Handler) DiscoveryMining(w http.ResponseWriter, r *http.Request) {
	log.Infof("DiscoveryMining request received for month %s", r.URL.Query().Get("month"))
	// This should ideally be protected or internal
	monthStr := r.URL.Query().Get("month")
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		http.Error(w, "Invalid month", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "Discovery mining started for month %d", month)

	go h.performDiscoveryMining(month)
}

func (h *Handler) performDiscoveryMining(month int) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Panic in performDiscoveryMining: %v", r)
		}
	}()
	log.SetPrefix("discovery-mining")
	log.Infof("Starting mining for month %d", month)

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}
	log.Infof("Agent URL: %s", agentURL)

	appName := "discovery_agent"
	userID := "system"
	sessionID := fmt.Sprintf("discovery_%d_%d", month, time.Now().Unix())

	client := h.AgentClient

	// 1. Create Session
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	log.Infof("Creating agent session: %s", createSessionURL)
	respSession, err := client.Post(createSessionURL, "application/json", nil)
	if err != nil {
		log.Infof("Failed to create agent session: %v", err)
	} else if respSession != nil {
		respSession.Body.Close()
	}

	// 2. Run Agent
	monthName := time.Month(month).String()
	prompt := fmt.Sprintf("Identify top sailing destinations and deep cuts for the month of %s.", monthName)

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
		log.Infof("Failed to call discovery agent: %v", err)
		return
	}
	defer resp.Body.Close()

	var events []AgentEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		log.Infof("Failed to decode agent response: %v", err)
		return
	}

	var responseText string
	for _, e := range events {
		if e.Content.Role == "model" && len(e.Content.Parts) > 0 {
			responseText = e.Content.Parts[0].Text
		}
	}

	if responseText == "" {
		log.Infof("No response from discovery agent")
		return
	}

	responseText = cleanJSON(responseText)

	var output []DiscoveryRegionOutput
	if err := json.Unmarshal([]byte(responseText), &output); err != nil {
		log.Infof("Failed to unmarshal discovery agent JSON: %v. Raw: %s", err, responseText)
		return
	}

	ctx := context.Background()
	for _, reg := range output {
		region := &models.SailingRegion{
			Name:     reg.Name,
			Geometry: models.RawJSON(reg.Geometry),
			Type:     reg.Type,
		}

		if err := h.DB.UpsertRegion(ctx, region); err != nil {
			log.Errorf("Failed to upsert region %s: %v", reg.Name, err)
			continue
		}

		seasonality := &models.RegionSeasonality{
			RegionID:          region.ID,
			Month:             month,
			SuitabilityScore:  reg.SuitabilityScore,
			IsHiddenGem:       reg.IsHiddenGem,
			Summary:           reg.Summary,
			DeepCutReasoning:  reg.DeepCutReasoning,
			AvgWindSpeedKnots: reg.AvgWindSpeedKnots,
			AvgTempC:          reg.AvgTempC,
		}

		if err := h.DB.UpsertSeasonality(ctx, seasonality); err != nil {
			log.Errorf("Failed to upsert seasonality for %s: %v", reg.Name, err)
		}
	}

	log.Infof("Discovery mining complete for month %d. Processed %d regions.", month, len(output))
}
