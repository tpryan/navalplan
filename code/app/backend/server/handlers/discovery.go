package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	appcontext "app/context"
	"app/models"

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
		writeError(w, http.StatusInternalServerError, "Failed to list regions")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(regions)
}

func (h *Handler) DeleteDiscoveryRegionSeasonality(w http.ResponseWriter, r *http.Request) {
	// Check for admin
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil || !person.IsAdmin {
		writeError(w, http.StatusForbidden, "Forbidden: Admins only")
		return
	}

	regionIDStr := r.PathValue("regionID")
	monthStr := r.PathValue("month")

	regionID, err := strconv.Atoi(regionIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid region ID")
		return
	}

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		writeError(w, http.StatusBadRequest, "Invalid month")
		return
	}

	if err := h.DB.DeleteSeasonality(r.Context(), regionID, month); err != nil {
		slog.ErrorContext(r.Context(), "Failed to delete seasonality", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete seasonality")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DiscoveryMining(w http.ResponseWriter, r *http.Request) {
	monthStr := r.URL.Query().Get("month")
	slog.InfoContext(r.Context(), fmt.Sprintf("DiscoveryMining request received for month %s", monthStr))

	if monthStr == "all" {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "Discovery mining started for all months")

		go func() {
			// 4 hours: 12 months × ~20 minutes each (generous for LLM calls).
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
			defer cancel()
			slog.Info("[discovery-mining] Starting full year mining cycle...")
			for m := 1; m <= 12; m++ {
				if ctx.Err() != nil {
					slog.Error("[discovery-mining] Context cancelled before completing all months", "completed", m-1)
					return
				}
				h.performDiscoveryMining(ctx, m)
			}
			slog.Info("[discovery-mining] Full year mining cycle complete.")
		}()
		return
	}

	// This should ideally be protected or internal
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		month = int(time.Now().Month())
		slog.InfoContext(r.Context(), fmt.Sprintf("Defaulting to current month: %d", month))
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "Discovery mining started for month %d", month)

	go func() {
		// 20 minutes per single-month run.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		h.performDiscoveryMining(ctx, month)
	}()
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

func (h *Handler) performDiscoveryMining(ctx context.Context, month int) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "[discovery-mining] Panic", "recover", r)
		}
	}()
	slog.InfoContext(ctx, fmt.Sprintf("[discovery-mining] Starting mining for month %d", month))

	agentURL := h.AgentURL
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	appName := "commodore"
	userID := "system"
	sessionID := fmt.Sprintf("discovery_%d_%d", month, time.Now().Unix())

	client := h.AgentClient

	// 1. Create Session with initial state
	createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
	slog.InfoContext(ctx, fmt.Sprintf("[discovery-mining] Creating agent session: %s", createSessionURL))

	monthName := time.Month(month).String()
	state := map[string]any{
		"Month": monthName,
	}
	stateJSON, _ := json.Marshal(map[string]any{"state": state})

	sessionReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createSessionURL, bytes.NewBuffer(stateJSON))
	if err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to build session request", "error", err)
		return
	}
	sessionReq.Header.Set("Content-Type", "application/json")
	respSession, err := client.Do(sessionReq)
	if err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to create agent session", "error", err)
		return
	}
	respSession.Body.Close()
	if respSession.StatusCode >= http.StatusInternalServerError {
		slog.ErrorContext(ctx, "[discovery-mining] Agent session creation returned server error", "status", respSession.StatusCode)
		return
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
	slog.InfoContext(ctx, "[discovery-mining] Calling agent /api/run...")
	runReq, err := http.NewRequestWithContext(ctx, http.MethodPost, agentURL+"/api/run", bytes.NewBuffer(jsonData))
	if err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to build run request", "error", err)
		return
	}
	runReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(runReq)
	if err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to call discovery agent", "error", err)
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to read agent response body", "error", err)
		return
	}

	var events []AgentEvent
	if err := json.Unmarshal(bodyBytes, &events); err != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("[discovery-mining] Failed to decode agent response. Raw body: %s", string(bodyBytes)), "error", err)
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
		slog.ErrorContext(ctx, "[discovery-mining] No response text found in events", "events", events)
		return
	}

	slog.InfoContext(ctx, "[discovery-mining] Raw agent response", "response", responseText)

	cleanedResponseText := cleanJSON(responseText)

	var output []DiscoveryRegionOutput
	if err := json.Unmarshal([]byte(cleanedResponseText), &output); err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to unmarshal discovery agent JSON", "error", err, "cleaned", cleanedResponseText, "raw", responseText)
		return
	}

	// Load existing regions ACTIVE IN THIS MONTH for intelligent replacement
	activeRegions, err := h.DB.ListRegionsByMonth(ctx, month)
	if err != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("[discovery-mining] Failed to load active regions for month %d", month), "error", err)
	}

	type activeReg struct {
		ID       int64
		Name     string
		Tier     string
		IsHidden bool
		Geom     *geojson.Geometry
	}
	var parsedActive []activeReg
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

	for _, reg := range output {
		// Check for spatial duplicates
		newGeom, err := geojson.UnmarshalGeometry(reg.Geometry)
		if err != nil {
			slog.ErrorContext(ctx, fmt.Sprintf("[discovery-mining] Failed to parse geometry for new region %s", reg.Name), "error", err)
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
					slog.InfoContext(ctx, fmt.Sprintf("[discovery-mining] Replacing existing '%s' (Tier: %s) with new superior '%s' (Tier: %s) (IoU: %.2f, Cont: %.2f)",
						ex.Name, ex.Tier, reg.Name, reg.Tier, iou, containment))

					// Delete the old seasonality
					if err := h.DB.DeleteSeasonality(ctx, int(ex.ID), month); err != nil {
						slog.ErrorContext(ctx, fmt.Sprintf("[discovery-mining] Failed to remove inferior region %s", ex.Name), "error", err)
					}

					// Remove from parsedActive so we don't match against it again
					parsedActive[i].Name = ""
				} else {
					slog.InfoContext(ctx, fmt.Sprintf("[discovery-mining] Skipping new region '%s' (Tier: %s) in favor of existing '%s' (Tier: %s) (IoU: %.2f, Cont: %.2f)",
						reg.Name, reg.Tier, ex.Name, ex.Tier, iou, containment))
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
			slog.ErrorContext(ctx, fmt.Sprintf("[discovery-mining] Failed to upsert region %s", reg.Name), "error", err)
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
			slog.ErrorContext(ctx, fmt.Sprintf("[discovery-mining] Failed to upsert seasonality for %s", reg.Name), "error", err)
		}
	}

	slog.InfoContext(ctx, fmt.Sprintf("[discovery-mining] Discovery mining complete for month %d. Processed %d regions.", month, len(output)))
}
