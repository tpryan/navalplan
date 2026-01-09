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

	appcontext "app/context"
	"app/models"

	"github.com/charmbracelet/log"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
)

type DiscoveryRegionOutput struct {
	Name              string          `json:"name"`
	Type              string          `json:"type"`
	IsHiddenGem       bool            `json:"is_hidden_gem"`
	Tier              string          `json:"tier"` // "Standard", "Hidden Gem", "Regional Favorite"
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

func (h *Handler) DeleteDiscoveryRegionSeasonality(w http.ResponseWriter, r *http.Request) {
	// Check for admin
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil || !person.IsAdmin {
		http.Error(w, "Forbidden: Admins only", http.StatusForbidden)
		return
	}

	regionIDStr := r.PathValue("regionID")
	monthStr := r.PathValue("month")

	regionID, err := strconv.Atoi(regionIDStr)
	if err != nil {
		http.Error(w, "Invalid region ID", http.StatusBadRequest)
		return
	}

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		http.Error(w, "Invalid month", http.StatusBadRequest)
		return
	}

	if err := h.DB.DeleteSeasonality(r.Context(), regionID, month); err != nil {
		log.Errorf("Failed to delete seasonality: %v", err)
		http.Error(w, "Failed to delete seasonality", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

func computeBoundsStats(b1, b2 orb.Bound) (iou, containment, sizeRatio float64) {
	// Intersect bounds
	minX := max(b1.Min.X(), b2.Min.X())
	minY := max(b1.Min.Y(), b2.Min.Y())
	maxX := min(b1.Max.X(), b2.Max.X())
	maxY := min(b1.Max.Y(), b2.Max.Y())

	var intersectArea float64
	if minX < maxX && minY < maxY {
		intersectArea = (maxX - minX) * (maxY - minY)
	}

	area1 := (b1.Max.X() - b1.Min.X()) * (b1.Max.Y() - b1.Min.Y())
	area2 := (b2.Max.X() - b2.Min.X()) * (b2.Max.Y() - b2.Min.Y())

	unionArea := area1 + area2 - intersectArea

	if unionArea > 0 {
		iou = intersectArea / unionArea
	}

	minArea := min(area1, area2)
	maxArea := max(area1, area2)

	if minArea > 0 {
		containment = intersectArea / minArea
	}

	if maxArea > 0 {
		sizeRatio = minArea / maxArea
	}

	return iou, containment, sizeRatio
}

func getTierPriority(tier string, isHiddenGem bool) int {
	if tier == "Challenging" {
		return 4
	}
	if isHiddenGem || tier == "Hidden Gem" || tier == "Deep Cut" {
		return 3
	}
	if tier == "Regional Favorite" {
		return 2
	}
	// Standard or unknown
	return 1
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
	prompt := fmt.Sprintf("Identify top sailing destinations, deep cuts, and challenging sailing areas (for expert sailors, such as San Francisco Bay) for the month of %s. Ensure GLOBAL coverage (North America, Europe, Asia, Oceania, Caribbean). Return JSON only.", monthName)

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

	// Load existing regions ACTIVE IN THIS MONTH for intelligent replacement
	activeRegions, err := h.DB.ListRegionsByMonth(ctx, month)
	if err != nil {
		log.Errorf("[discovery-mining] Failed to load active regions for month %d: %v", month, err)
	}

	type activeReg struct {
		ID       int64
		Name     string
		Tier     string
		IsHidden bool
		Geom     *geojson.Geometry
	}
	var parsedActive []activeReg
	if activeRegions != nil {
		for _, er := range activeRegions {
			g, err := geojson.UnmarshalGeometry(er.Geometry)
			if err == nil {
				parsedActive = append(parsedActive, activeReg{
					ID:       er.SailingRegion.ID,
					Name:     er.Name,
					Tier:     er.Tier,
					IsHidden: er.IsHiddenGem,
					Geom:     g,
				})
			}
		}
	}

	for _, reg := range output {
		// Check for spatial duplicates
		newGeom, err := geojson.UnmarshalGeometry(reg.Geometry)
		if err != nil {
			log.Errorf("[discovery-mining] Failed to parse geometry for new region %s: %v", reg.Name, err)
			continue
		}

		shouldSkip := false
		for i, ex := range parsedActive {
			// If names match, we assume it's an update to the same region, so we proceed (UpsertRegion will handle it).
			if ex.Name == reg.Name {
				continue
			}

			// If names differ, check for spatial overlap.
			b1 := newGeom.Geometry().Bound()
			b2 := ex.Geom.Geometry().Bound()
			iou, containment, sizeRatio := computeBoundsStats(b1, b2)

			// Conflict Criteria:
			// 1. IoU > 0.5 (Significant direct overlap)
			// 2. Containment > 0.8 (One is mostly inside other) AND SizeRatio > 0.3 (They are comparable in size, avoiding "St Lucia vs Caribbean")
			isDuplicate := false
			if iou > 0.5 {
				isDuplicate = true
			} else if containment > 0.8 && sizeRatio > 0.3 {
				isDuplicate = true
			}

			if isDuplicate {
				// Conflict! Compare priorities.
				newPriority := getTierPriority(reg.Tier, reg.IsHiddenGem)
				oldPriority := getTierPriority(ex.Tier, ex.IsHidden)

				// Resolution:
				// If New is HIGHER priority, we replace Old.
				// If New is EQUAL priority, we keep Old (stable).
				// If New is LOWER priority, we keep Old.

				if newPriority > oldPriority {
					log.Infof("[discovery-mining] Replacing existing '%s' (Tier: %s) with new superior '%s' (Tier: %s) (IoU: %.2f, Cont: %.2f)",
						ex.Name, ex.Tier, reg.Name, reg.Tier, iou, containment)

					// Delete the old seasonality
					if err := h.DB.DeleteSeasonality(ctx, int(ex.ID), month); err != nil {
						log.Errorf("[discovery-mining] Failed to remove inferior region %s: %v", ex.Name, err)
					}

					// Remove from parsedActive so we don't match against it again
					parsedActive[i].Name = ""
				} else {
					log.Infof("[discovery-mining] Skipping new region '%s' (Tier: %s) in favor of existing '%s' (Tier: %s) (IoU: %.2f, Cont: %.2f)",
						reg.Name, reg.Tier, ex.Name, ex.Tier, iou, containment)
					shouldSkip = true
					break
				}
			}
		}

		if shouldSkip {
			continue
		}

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
			Tier:              reg.Tier,
			Summary:           reg.Summary,
			DeepCutReasoning:  reg.DeepCutReasoning,
			AvgWindSpeedKnots: reg.AvgWindSpeedKnots,
			AvgTempC:          reg.AvgTempC,
		}

		// Backward compatibility: If Tier is set, derive IsHiddenGem
		if seasonality.Tier != "" {
			seasonality.IsHiddenGem = (seasonality.Tier == "Hidden Gem")
		} else {
			// If Tier missing (old agent output?), derive Tier from IsHiddenGem
			if seasonality.IsHiddenGem {
				seasonality.Tier = "Hidden Gem"
			} else {
				seasonality.Tier = "Standard"
			}
		}

		if err := h.DB.UpsertSeasonality(ctx, seasonality); err != nil {
			log.Errorf("[discovery-mining] Failed to upsert seasonality for %s: %v", reg.Name, err)
		}
	}

	log.Infof("[discovery-mining] Discovery mining complete for month %d. Processed %d regions.", month, len(output))
}
