package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	monthStr := r.URL.Query().Get("month")
	log.Infof("DiscoveryMining request received for month %s", monthStr)

	if monthStr == "all" {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "Discovery mining started for all months")

		go func() {
			log.Info("[discovery-mining] Starting full year mining cycle...")
			for m := 1; m <= 12; m++ {
				h.performDiscoveryMining(m)
			}
			log.Info("[discovery-mining] Full year mining cycle complete.")
		}()
		return
	}

	// This should ideally be protected or internal
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		month = int(time.Now().Month())
		log.Infof("Defaulting to current month: %d", month)
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "Discovery mining started for month %d", month)

	go h.performDiscoveryMining(month)
}

func (h *Handler) performDiscoveryMining(month int) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[discovery-mining] Panic: %v", r)
		}
	}()
	log.Infof("[discovery-mining] Starting mining for month %d", month)

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "discovery_agent"
	userID := "system"
	sessionID := fmt.Sprintf("discovery_%d_%d", month, time.Now().Unix())

	client := h.AgentClient

	// 1. Create Session with initial state
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	log.Infof("[discovery-mining] Creating agent session: %s", createSessionURL)

	monthName := time.Month(month).String()
	state := map[string]any{
		"Month": monthName,
	}
	stateJSON, _ := json.Marshal(map[string]any{"state": state})

	respSession, err := client.Post(createSessionURL, "application/json", bytes.NewBuffer(stateJSON))
	if err != nil {
		log.Errorf("[discovery-mining] Failed to create agent session: %v", err)
	} else if respSession != nil {
		respSession.Body.Close()
	}

	// 2. Run Agent
	prompt := fmt.Sprintf("Identify top sailing destinations and deep cuts for the month of %s. Return JSON only.", monthName)

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
	log.Infof("[discovery-mining] Calling agent /api/run...")
	resp, err := client.Post(agentURL+"/api/run", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Errorf("[discovery-mining] Failed to call discovery agent: %v", err)
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("[discovery-mining] Failed to read agent response body: %v", err)
		return
	}

	var events []AgentEvent
	if err := json.Unmarshal(bodyBytes, &events); err != nil {
		log.Errorf("[discovery-mining] Failed to decode agent response: %v. Raw body: %s", err, string(bodyBytes))
		return
	}

	// Find the model response
	var responseText string
	for _, e := range events {
		if e.Content.Role == "model" && len(e.Content.Parts) > 0 {
			responseText += e.Content.Parts[0].Text
		}
	}

	if responseText == "" {
		log.Errorf("[discovery-mining] No response text found in events. Full event log: %+v", events)
		return
	}

	log.Infof("[discovery-mining] Raw agent response: %s", responseText)

	cleanedResponseText := cleanJSON(responseText)

	var output []DiscoveryRegionOutput
	if err := json.Unmarshal([]byte(cleanedResponseText), &output); err != nil {
		log.Errorf("[discovery-mining] Failed to unmarshal discovery agent JSON: %v. Cleaned: %s. Raw: %s", err, cleanedResponseText, responseText)
		return
	}

	ctx := context.Background()

	// Clear old data for this month to ensure we replace it
	if err := h.DB.DeleteSeasonalityForMonth(ctx, month); err != nil {
		log.Errorf("[discovery-mining] Failed to clear old seasonality for month %d: %v", month, err)
		// We proceed anyway, or should we return? Proceeding might result in mix of old and new if upsert doesn't cover everything.
		// But UpsertSeasonality keys on (region_id, month), so effectively we just won't be deleting "stale" regions if we fail here.
	}

	for _, reg := range output {
		region := &models.SailingRegion{
			Name:     reg.Name,
			Geometry: models.RawJSON(reg.Geometry),
			Type:     reg.Type,
		}

		if err := h.DB.UpsertRegion(ctx, region); err != nil {
			log.Errorf("[discovery-mining] Failed to upsert region %s: %v", reg.Name, err)
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
			log.Errorf("[discovery-mining] Failed to upsert seasonality for %s: %v", reg.Name, err)
		}
	}

	log.Infof("[discovery-mining] Discovery mining complete for month %d. Processed %d regions.", month, len(output))
}