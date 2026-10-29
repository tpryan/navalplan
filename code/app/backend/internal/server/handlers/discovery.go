package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"app/internal/model"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
)

type DiscoveryRegionOutput struct {
	Name              string          `json:"name"`
	Type              string          `json:"type"`
	IsHiddenGem       bool            `json:"is_hidden_gem"`
	Tier              string          `json:"tier"`
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
	person := GetPersonFromContext(r.Context())
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

func (h *Handler) DeleteDiscoveryRegions(w http.ResponseWriter, r *http.Request) {
	monthStr := r.URL.Query().Get("month")
	slog.InfoContext(r.Context(), fmt.Sprintf("DeleteDiscoveryRegions request received for month %s", monthStr))

	var months []int
	if monthStr == "" || monthStr == "all" {
		monthStr = "all"
		for m := 1; m <= 12; m++ {
			months = append(months, m)
		}
	} else {
		m, err := strconv.Atoi(monthStr)
		if err != nil || m < 1 || m > 12 {
			writeError(w, http.StatusBadRequest, "Invalid month parameter")
			return
		}
		months = []int{m}
	}

	deletedCount := 0
	for _, m := range months {
		regions, err := h.DB.ListRegionsByMonth(r.Context(), m)
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to list discovery regions for deletion", "month", m, "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to delete discovery regions")
			return
		}
		for _, reg := range regions {
			if err := h.DB.DeleteSeasonality(r.Context(), int(reg.SailingRegion.ID), m); err != nil {
				slog.ErrorContext(r.Context(), "Failed to delete seasonality for region", "region_id", reg.SailingRegion.ID, "month", m, "error", err)
				writeError(w, http.StatusInternalServerError, "Failed to delete discovery regions")
				return
			}
			deletedCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"month":         monthStr,
		"deleted_count": deletedCount,
	})
}

func (h *Handler) DiscoveryMining(w http.ResponseWriter, r *http.Request) {
	monthStr := r.URL.Query().Get("month")
	slog.InfoContext(r.Context(), fmt.Sprintf("DiscoveryMining request received for month %s", monthStr))

	if monthStr == "all" {
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "Discovery mining started for all months")

		go func() {
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

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		month = int(time.Now().Month())
		slog.InfoContext(r.Context(), fmt.Sprintf("Defaulting to current month: %d", month))
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "Discovery mining started for month %d", month)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		h.performDiscoveryMining(ctx, month)
	}()
}

func normalizeRegionKey(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if idx := strings.Index(s, "("); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}
	if idx := strings.Index(s, ","); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}
	return s
}

// isRegionDuplicate detects duplicate or stacked macro discovery regions in the
// same month (e.g., two overlapping or adjacent "Society Islands" / "French
// Polynesia" polygons). Macro global discovery regions are hundreds of NM apart,
// so any bounding-box overlap or centers within 3.5 degrees (~210 NM) — or
// matching normalized region names — indicate a duplicate region.
func isRegionDuplicate(nameA, nameB string, b1, b2 orb.Bound) (bool, float64, float64) {
	iou, containment := computeBoundsStats(b1, b2)
	if iou > 0.05 || containment > 0.15 {
		return true, iou, containment
	}
	c1 := b1.Center()
	c2 := b2.Center()
	centerDistDeg := math.Hypot(c1.X()-c2.X(), c1.Y()-c2.Y())
	if centerDistDeg < 3.5 {
		return true, iou, containment
	}
	kA := normalizeRegionKey(nameA)
	kB := normalizeRegionKey(nameB)
	if kA != "" && kB != "" && (kA == kB || strings.Contains(kA, kB) || strings.Contains(kB, kA)) && centerDistDeg < 8.0 {
		return true, iou, containment
	}
	return false, iou, containment
}

// computeBoundsStats computes bounding-box Intersection-over-Union (IoU) and
// containment in raw degrees². Note: degree-squared areas distort with latitude
// and do not wrap across the ±180° antimeridian; this is a coarse screen paired
// with center-distance and normalized name checks in isRegionDuplicate.
func computeBoundsStats(b1, b2 orb.Bound) (iou, containment float64) {
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

	if minArea > 0 {
		containment = intersectArea / minArea
	}

	return iou, containment
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
	return 1
}

func (h *Handler) DiscoveryPruning(w http.ResponseWriter, r *http.Request) {
	monthStr := r.URL.Query().Get("month")
	slog.InfoContext(r.Context(), fmt.Sprintf("DiscoveryPruning request received for month %s", monthStr))

	var month int
	if monthStr == "all" {
		month = 0
	} else if monthStr != "" {
		m, err := strconv.Atoi(monthStr)
		if err != nil || m < 1 || m > 12 {
			writeError(w, http.StatusBadRequest, "Invalid month parameter")
			return
		}
		month = m
	} else {
		month = int(time.Now().Month())
	}

	prunedCount, err := h.PruneDuplicateRegions(r.Context(), month)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to prune duplicate discovery regions", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to prune duplicate regions")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":       "ok",
		"month":        monthStr,
		"pruned_count": prunedCount,
	})
}

func (h *Handler) PruneDuplicateRegions(ctx context.Context, month int) (int, error) {
	if month == 0 {
		totalPruned := 0
		var firstErr error
		failedMonths := 0
		for m := 1; m <= 12; m++ {
			n, err := h.PruneDuplicateRegions(ctx, m)
			if err != nil {
				slog.ErrorContext(ctx, "[discovery-prune] Error pruning month", "month", m, "error", err)
				failedMonths++
				if firstErr == nil {
					firstErr = err
				}
			}
			totalPruned += n
		}
		if failedMonths == 12 && firstErr != nil {
			return 0, firstErr
		}
		return totalPruned, nil
	}

	activeRegions, err := h.DB.ListRegionsByMonth(ctx, month)
	if err != nil {
		return 0, fmt.Errorf("failed to list regions for month %d: %w", month, err)
	}

	type parsedItem struct {
		region model.RegionWithSeasonality
		geom   *geojson.Geometry
	}

	var items []parsedItem
	for _, r := range activeRegions {
		g, err := geojson.UnmarshalGeometry(r.Geometry)
		if err == nil {
			items = append(items, parsedItem{region: r, geom: g})
		}
	}

	prunedCount := 0
	removedIDs := make(map[int64]bool)

	for i := 0; i < len(items); i++ {
		if removedIDs[items[i].region.SailingRegion.ID] {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			if removedIDs[items[j].region.SailingRegion.ID] {
				continue
			}

			itemA := items[i]
			itemB := items[j]

			if itemA.region.SailingRegion.ID == itemB.region.SailingRegion.ID || itemA.region.Name == itemB.region.Name {
				continue
			}

			b1 := itemA.geom.Geometry().Bound()
			b2 := itemB.geom.Geometry().Bound()
			dup, iou, containment := isRegionDuplicate(itemA.region.Name, itemB.region.Name, b1, b2)

			if dup {
				pA := getTierPriority(itemA.region.Tier, itemA.region.IsHiddenGem)
				pB := getTierPriority(itemB.region.Tier, itemB.region.IsHiddenGem)

				var removeTarget model.RegionWithSeasonality
				var keepTarget model.RegionWithSeasonality

				if pA > pB {
					keepTarget = itemA.region
					removeTarget = itemB.region
				} else if pB > pA {
					keepTarget = itemB.region
					removeTarget = itemA.region
				} else {
					if itemA.region.SuitabilityScore >= itemB.region.SuitabilityScore {
						keepTarget = itemA.region
						removeTarget = itemB.region
					} else {
						keepTarget = itemB.region
						removeTarget = itemA.region
					}
				}

				slog.InfoContext(ctx, "[discovery-prune] Removing duplicate region",
					"remove_name", removeTarget.Name, "remove_id", removeTarget.SailingRegion.ID, "remove_tier", removeTarget.Tier, "remove_score", removeTarget.SuitabilityScore,
					"keep_name", keepTarget.Name, "keep_id", keepTarget.SailingRegion.ID, "keep_tier", keepTarget.Tier, "keep_score", keepTarget.SuitabilityScore,
					"iou", iou, "containment", containment)

				if err := h.DB.DeleteSeasonality(ctx, int(removeTarget.SailingRegion.ID), month); err != nil {
					slog.ErrorContext(ctx, "[discovery-prune] Failed to delete seasonality for region", "region_id", removeTarget.SailingRegion.ID, "error", err)
				} else {
					removedIDs[removeTarget.SailingRegion.ID] = true
					prunedCount++
				}
			}
		}
	}

	return prunedCount, nil
}

func (h *Handler) performDiscoveryMining(ctx context.Context, month int) {
	start := time.Now()
	monthName := time.Month(month).String()
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "[discovery-mining] Panic during mining", "month", month, "month_name", monthName, "recover", r)
		}
	}()
	slog.InfoContext(ctx, "[discovery-mining] Starting mining", "month", month, "month_name", monthName)

	const appName = "commodore"
	const userID = "system"
	sessionID := fmt.Sprintf("discovery_%d_%d", month, time.Now().Unix())

	slog.InfoContext(ctx, "[discovery-mining] Creating agent session", "month", month, "session_id", sessionID)
	if err := h.Agent.CreateSession(ctx, appName, userID, sessionID, map[string]any{"Month": monthName}); err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to create agent session", "month", month, "month_name", monthName, "error", err, "duration", time.Since(start))
		return
	}

	existingRegionsForPrompt, _ := h.DB.ListRegionsByMonth(ctx, month)
	var existingNames []string
	for _, er := range existingRegionsForPrompt {
		if strings.TrimSpace(er.Name) != "" {
			existingNames = append(existingNames, er.Name)
		}
	}

	prompt := fmt.Sprintf("Identify top sailing destinations, deep cuts, and challenging sailing areas (for expert sailors, such as San Francisco Bay) for the month of %s. Ensure GLOBAL coverage (North America, Europe, Asia, Oceania, Caribbean). Return at most ONE region per archipelago, island group, or coastal basin, and ensure all bounding boxes are at least 300 NM (~5 degrees) apart. Return JSON only.", monthName)
	if len(existingNames) > 0 {
		prompt += fmt.Sprintf(" Existing regions already active for %s: [%s]. Do NOT output a second region covering the same archipelago, island group, or coast under a variant name or shifted bounding box.", monthName, strings.Join(existingNames, ", "))
	}

	slog.InfoContext(ctx, "[discovery-mining] Invoking commodore agent", "month", month, "month_name", monthName)
	agentStart := time.Now()
	responseText, err := h.Agent.RunSync(ctx, appName, userID, sessionID, prompt)
	agentDuration := time.Since(agentStart)
	if err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Agent run failed", "month", month, "month_name", monthName, "duration", agentDuration, "error", err)
		return
	}

	if responseText == "" {
		slog.ErrorContext(ctx, "[discovery-mining] No response text from agent", "month", month, "month_name", monthName, "duration", agentDuration)
		return
	}

	slog.InfoContext(ctx, "[discovery-mining] Agent returned response", "month", month, "month_name", monthName, "bytes", len(responseText), "duration", agentDuration)

	cleanedResponseText := cleanJSON(responseText)

	var output []DiscoveryRegionOutput
	if err := json.Unmarshal([]byte(cleanedResponseText), &output); err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to unmarshal discovery agent JSON", "month", month, "month_name", monthName, "error", err, "cleaned", cleanedResponseText, "raw", responseText)
		return
	}

	slog.InfoContext(ctx, "[discovery-mining] Saving candidate regions", "month", month, "month_name", monthName, "count", len(output))

	activeRegions, err := h.DB.ListRegionsByMonth(ctx, month)
	if err != nil {
		slog.ErrorContext(ctx, "[discovery-mining] Failed to load active regions; aborting to avoid duplicate insertion", "month", month, "error", err)
		return
	}

	type activeReg struct {
		ID       int64
		Name     string
		Tier     string
		IsHidden bool
		Replaced bool
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
		newGeom, err := geojson.UnmarshalGeometry(reg.Geometry)
		if err != nil {
			slog.ErrorContext(ctx, "[discovery-mining] Failed to parse geometry for new region", "region", reg.Name, "error", err)
			continue
		}

		shouldSkip := false
		for i, ex := range parsedActive {
			if ex.Replaced {
				continue
			}

			if ex.Name == reg.Name {
				continue
			}

			b1 := newGeom.Geometry().Bound()
			b2 := ex.Geom.Geometry().Bound()
			isDuplicate, iou, containment := isRegionDuplicate(reg.Name, ex.Name, b1, b2)

			if isDuplicate {
				newPriority := getTierPriority(reg.Tier, reg.IsHiddenGem)
				oldPriority := getTierPriority(ex.Tier, ex.IsHidden)

				if newPriority > oldPriority {
					slog.InfoContext(ctx, "[discovery-mining] Replacing existing region with superior candidate",
						"existing_name", ex.Name, "existing_tier", ex.Tier, "new_name", reg.Name, "new_tier", reg.Tier, "iou", iou, "containment", containment)

					if err := h.DB.DeleteSeasonality(ctx, int(ex.ID), month); err != nil {
						slog.ErrorContext(ctx, "[discovery-mining] Failed to remove inferior region", "region", ex.Name, "error", err)
					}

					parsedActive[i].Replaced = true
				} else {
					slog.InfoContext(ctx, "[discovery-mining] Skipping new region in favor of existing",
						"new_name", reg.Name, "new_tier", reg.Tier, "existing_name", ex.Name, "existing_tier", ex.Tier, "iou", iou, "containment", containment)
					shouldSkip = true
					break
				}
			}
		}

		if shouldSkip {
			continue
		}

		region := &model.SailingRegion{
			Name:     reg.Name,
			Geometry: model.RawJSON(reg.Geometry),
			Type:     reg.Type,
		}

		if err := h.DB.UpsertRegion(ctx, region); err != nil {
			slog.ErrorContext(ctx, "[discovery-mining] Failed to upsert region", "region", reg.Name, "error", err)
			continue
		}

		seasonality := &model.RegionSeasonality{
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

		if seasonality.Tier != "" {
			seasonality.IsHiddenGem = (seasonality.Tier == "Hidden Gem")
		} else {
			if seasonality.IsHiddenGem {
				seasonality.Tier = "Hidden Gem"
			} else {
				seasonality.Tier = "Standard"
			}
		}

		if err := h.DB.UpsertSeasonality(ctx, seasonality); err != nil {
			slog.ErrorContext(ctx, "[discovery-mining] Failed to upsert seasonality", "region", reg.Name, "error", err)
		} else {
			parsedActive = append(parsedActive, activeReg{
				ID:       region.ID,
				Name:     region.Name,
				Tier:     seasonality.Tier,
				IsHidden: seasonality.IsHiddenGem,
				Geom:     newGeom,
			})
		}
	}

	if pruned, err := h.PruneDuplicateRegions(ctx, month); err == nil && pruned > 0 {
		slog.InfoContext(ctx, "[discovery-mining] Pruned duplicate regions", "month", month, "month_name", monthName, "pruned", pruned)
	}

	slog.InfoContext(ctx, "[discovery-mining] Completed mining", "month", month, "month_name", monthName, "duration", time.Since(start), "saved", len(output))
}
