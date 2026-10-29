package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/tpryan/navalplan/services/researcher/internal/agent"
	"github.com/tpryan/navalplan/services/researcher/internal/config"
	"github.com/tpryan/navalplan/services/researcher/internal/prompt"
	"github.com/tpryan/navalplan/services/researcher/internal/session"
	"github.com/tpryan/navalplan/services/researcher/internal/tool"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	adktool "google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

// ToolInvocation records a single tool call made by an agent during an eval case.
type ToolInvocation struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// evalToolRecorder is a concurrency-safe recorder for tool invocations during a test case.
type evalToolRecorder struct {
	mu    sync.Mutex
	calls []ToolInvocation
}

func (r *evalToolRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

func (r *evalToolRecorder) BeforeTool(_ adkagent.Context, tl adktool.Tool, args map[string]any) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make(map[string]any, len(args))
	for k, v := range args {
		copied[k] = v
	}
	r.calls = append(r.calls, ToolInvocation{
		Name: tl.Name(),
		Args: copied,
	})
	return nil, nil
}

func (r *evalToolRecorder) Snapshot() []ToolInvocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ToolInvocation, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *evalToolRecorder) ToolNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]bool, len(r.calls))
	var names []string
	for _, c := range r.calls {
		if !seen[c.Name] {
			seen[c.Name] = true
			names = append(names, c.Name)
		}
	}
	return names
}

// EvalCase defines a single deterministic evaluation case for one of the 8 agents.
type EvalCase struct {
	AgentName       string
	CaseID          string
	Description     string
	Prompt          string
	RequiredTools   []string
	AnyOfToolGroups [][]string
	ScoreFunc       func(rawText string, calls []ToolInvocation) (schemaScore, domainScore float64, failures []string)
}

// CaseEvalResult captures the deterministic evaluation scores and diagnostics for one case.
type CaseEvalResult struct {
	AgentName      string        `json:"agent_name"`
	CaseID         string        `json:"case_id"`
	Passed         bool          `json:"passed"`
	SchemaScore    float64       `json:"schema_score"`
	ToolScore      float64       `json:"tool_score"`
	DomainScore    float64       `json:"domain_score"`
	OverallScore   float64       `json:"overall_score"`
	Duration       time.Duration `json:"duration_ns"`
	DurationSec    float64       `json:"duration_s"`
	ToolsCalled    []string      `json:"tools_called"`
	FailureReasons []string      `json:"failure_reasons,omitempty"`
	OutputSample   string        `json:"output_sample,omitempty"`
}

func loadEvalEnv() {
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load("../../../.env")
}

func shouldRunEval(target string) bool {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("RUN_EVAL"))); v == "1" || v == "true" {
		return true
	}
	if f := flag.Lookup("test.run"); f != nil {
		pattern := f.Value.String()
		if strings.Contains(pattern, target) || strings.Contains(pattern, "TestEval") {
			return true
		}
	}
	return false
}

// extractJSONPayload strips optional markdown code fences and locates the outermost
// valid JSON object or array in raw model text.
func extractJSONPayload(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "```json"); idx >= 0 {
		rest := trimmed[idx+len("```json"):]
		if end := strings.Index(rest, "```"); end >= 0 {
			candidate := strings.TrimSpace(rest[:end])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	if idx := strings.Index(trimmed, "```"); idx >= 0 {
		rest := trimmed[idx+len("```"):]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 && nl < 15 && !strings.ContainsAny(rest[:nl], "{[") {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			candidate := strings.TrimSpace(rest[:end])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	firstObj := strings.IndexByte(trimmed, '{')
	lastObj := strings.LastIndexByte(trimmed, '}')
	firstArr := strings.IndexByte(trimmed, '[')
	lastArr := strings.LastIndexByte(trimmed, ']')

	var objCandidate, arrCandidate string
	if firstObj >= 0 && lastObj > firstObj {
		objCandidate = strings.TrimSpace(trimmed[firstObj : lastObj+1])
	}
	if firstArr >= 0 && lastArr > firstArr {
		arrCandidate = strings.TrimSpace(trimmed[firstArr : lastArr+1])
	}

	if firstObj >= 0 && (firstArr < 0 || firstObj < firstArr) {
		if objCandidate != "" && json.Valid([]byte(objCandidate)) {
			return objCandidate
		}
		if arrCandidate != "" && json.Valid([]byte(arrCandidate)) {
			return arrCandidate
		}
		if objCandidate != "" {
			return objCandidate
		}
	} else if firstArr >= 0 {
		if arrCandidate != "" && json.Valid([]byte(arrCandidate)) {
			return arrCandidate
		}
		if objCandidate != "" && json.Valid([]byte(objCandidate)) {
			return objCandidate
		}
		if arrCandidate != "" {
			return arrCandidate
		}
	}
	return trimmed
}

// scoreToolTrajectory computes a 0-100 tool trajectory score from required tools
// and any-of tool groups.
func scoreToolTrajectory(calls []ToolInvocation, required []string, anyOfGroups [][]string) (float64, []string) {
	totalChecks := len(required) + len(anyOfGroups)
	if totalChecks == 0 {
		return 100.0, nil
	}
	calledSet := make(map[string]bool, len(calls))
	for _, c := range calls {
		calledSet[c.Name] = true
	}
	passed := 0
	var failures []string
	for _, req := range required {
		if calledSet[req] {
			passed++
		} else {
			failures = append(failures, fmt.Sprintf("missing required tool call %q", req))
		}
	}
	for _, group := range anyOfGroups {
		matched := false
		for _, candidate := range group {
			if calledSet[candidate] {
				matched = true
				break
			}
		}
		if matched {
			passed++
		} else {
			failures = append(failures, fmt.Sprintf("missing any-of tool call from [%s]", strings.Join(group, ", ")))
		}
	}
	return (float64(passed) / float64(totalChecks)) * 100.0, failures
}

// evaluateCaseOutput runs the deterministic tool, schema, and domain scorers on a completed case.
func evaluateCaseOutput(tc EvalCase, rawText string, calls []ToolInvocation, duration time.Duration, runErr error) CaseEvalResult {
	var toolNames []string
	seen := make(map[string]bool, len(calls))
	for _, c := range calls {
		if !seen[c.Name] {
			seen[c.Name] = true
			toolNames = append(toolNames, c.Name)
		}
	}

	if runErr != nil {
		return CaseEvalResult{
			AgentName:      tc.AgentName,
			CaseID:         tc.CaseID,
			Passed:         false,
			Duration:       duration,
			DurationSec:    duration.Seconds(),
			ToolsCalled:    toolNames,
			FailureReasons: []string{fmt.Sprintf("agent execution error: %v", runErr)},
		}
	}

	toolScore, toolFailures := scoreToolTrajectory(calls, tc.RequiredTools, tc.AnyOfToolGroups)
	schemaScore, domainScore, contentFailures := tc.ScoreFunc(rawText, calls)
	failures := append(toolFailures, contentFailures...)

	overall := 0.35*schemaScore + 0.25*toolScore + 0.40*domainScore
	passed := len(failures) == 0 && schemaScore >= 80.0 && toolScore >= 80.0 && domainScore >= 75.0 && overall >= 80.0

	return CaseEvalResult{
		AgentName:      tc.AgentName,
		CaseID:         tc.CaseID,
		Passed:         passed,
		SchemaScore:    schemaScore,
		ToolScore:      toolScore,
		DomainScore:    domainScore,
		OverallScore:   overall,
		Duration:       duration,
		DurationSec:    duration.Seconds(),
		ToolsCalled:    toolNames,
		FailureReasons: failures,
		OutputSample:   buildOutputSample(tc.AgentName, rawText),
	}
}

// --- Deterministic Agent Output Scorers ---

func scoreHarbourmasterOutput(raw string, requireTiming bool) (float64, float64, []string) {
	cleaned := extractJSONPayload(raw)
	var data map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err != nil {
		return 0, 0, []string{fmt.Sprintf("invalid JSON: %v", err)}
	}

	var failures []string
	schemaChecks := 0
	totalSchema := 6

	if s, ok := data["location_name"].(string); ok && strings.TrimSpace(s) != "" {
		schemaChecks++
	} else {
		failures = append(failures, "missing non-empty location_name")
	}
	if ws, ok := data["weather_summary"].(map[string]any); ok && len(ws) > 0 {
		schemaChecks++
	} else {
		failures = append(failures, "missing weather_summary object")
	}
	if sp, ok := data["sun_phase"].(map[string]any); ok && sp["sunrise"] != nil && sp["sunset"] != nil {
		schemaChecks++
	} else {
		failures = append(failures, "missing sun_phase.sunrise/sunset")
	}
	if td, ok := data["tides"].(map[string]any); ok && td["events"] != nil {
		schemaChecks++
	} else {
		failures = append(failures, "missing tides object with events")
	}
	pn, hasPN := data["pilot_notes"].(map[string]any)
	if hasPN && len(pn) > 0 {
		schemaChecks++
	} else {
		failures = append(failures, "missing pilot_notes object")
	}
	facs, hasFacs := data["facilities"].([]any)
	if hasFacs {
		schemaChecks++
	} else {
		failures = append(failures, "missing facilities array")
	}
	schemaScore := (float64(schemaChecks) / float64(totalSchema)) * 100.0

	domainChecks := 0
	totalDomain := 3
	if len(facs) >= 3 {
		domainChecks++
	} else {
		failures = append(failures, fmt.Sprintf("expected at least 3 facilities, got %d", len(facs)))
	}
	if hasPN {
		ov, _ := pn["overview"].(string)
		ac, _ := pn["approach_and_channels"].(string)
		if strings.TrimSpace(ov) != "" && strings.TrimSpace(ac) != "" {
			domainChecks++
		} else {
			failures = append(failures, "pilot_notes missing overview or approach_and_channels")
		}
	}
	validCoords := 0
	for _, rawFac := range facs {
		if fm, ok := rawFac.(map[string]any); ok {
			lat, _ := toFloat64(fm["latitude"])
			lng, _ := toFloat64(fm["longitude"])
			if lat != 0 && lng != 0 {
				validCoords++
			}
		}
	}
	if len(facs) > 0 && validCoords > 0 {
		domainChecks++
	} else {
		failures = append(failures, "facilities missing non-zero coordinates")
	}

	if requireTiming {
		totalDomain += 4
		timing, ok := pn["departure_and_arrival_timing"].(map[string]any)
		if !ok {
			failures = append(failures, "pilot_notes.departure_and_arrival_timing missing")
		} else {
			for _, field := range []string{
				"recommended_departure_window",
				"departure_tide_constraint",
				"arrival_outlook",
				"night_entry_and_aids_warning",
			} {
				if v, ok := timing[field].(string); ok && strings.TrimSpace(v) != "" {
					domainChecks++
				} else {
					failures = append(failures, fmt.Sprintf("departure_and_arrival_timing.%s is empty", field))
				}
			}
		}
	}

	domainScore := (float64(domainChecks) / float64(totalDomain)) * 100.0
	return schemaScore, domainScore, failures
}

func scoreSpecialistOutput(raw string, expectedBases, expectedStops []string) (float64, float64, []string) {
	cleaned := extractJSONPayload(raw)
	var data map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err != nil {
		return 0, 0, []string{fmt.Sprintf("invalid JSON: %v", err)}
	}

	recs, ok := data["recommendations"].([]any)
	if !ok || len(recs) == 0 {
		return 0, 0, []string{"missing or empty recommendations array"}
	}

	var failures []string
	validItems := 0
	for _, item := range recs {
		rm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := rm["name"].(string)
		typ, _ := rm["type"].(string)
		desc, _ := rm["description"].(string)
		if strings.TrimSpace(name) != "" && strings.TrimSpace(typ) != "" && strings.TrimSpace(desc) != "" {
			validItems++
		}
	}
	schemaScore := (float64(validItems) / float64(len(recs))) * 100.0
	if validItems < len(recs) {
		failures = append(failures, fmt.Sprintf("%d/%d recommendations missing name/type/description", len(recs)-validItems, len(recs)))
	}

	if len(expectedBases) == 0 && len(expectedStops) == 0 {
		if len(recs) < 5 {
			failures = append(failures, fmt.Sprintf("expected at least 5 recommendations, got %d", len(recs)))
			return schemaScore, 50.0, failures
		}
		return schemaScore, 100.0, failures
	}

	baseHits := 0
	stopHits := 0
	falsePositiveBases := 0

	for _, item := range recs {
		rm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nameLower := strings.ToLower(fmt.Sprint(rm["name"]))
		role := strings.TrimSpace(fmt.Sprint(rm["voyage_role"]))
		stay, _ := rm["stay_details"].(map[string]any)
		isCharterBase, _ := stay["is_charter_base"].(bool)
		var companies []any
		if stay != nil {
			companies, _ = stay["charter_companies"].([]any)
		}

		matchedBase := false
		for _, baseToken := range expectedBases {
			bt := strings.ToLower(baseToken)
			// Match base tokens against the recommendation's OWN name (never description/reasoning,
			// which often mention nearby hubs like "within cruising range of Newport").
			if strings.Contains(nameLower, bt) {
				matchedBase = true
				break
			}
		}
		matchedStop := false
		for _, stopToken := range expectedStops {
			st := strings.ToLower(stopToken)
			if strings.Contains(nameLower, st) {
				matchedStop = true
				break
			}
		}

		if role == "start_end_base" || isCharterBase || len(companies) > 0 {
			if matchedStop && !matchedBase {
				falsePositiveBases++
				failures = append(failures, fmt.Sprintf("false-positive start_end_base/charter base on island/stopover %q (role=%q is_charter_base=%v companies=%v)", rm["name"], role, isCharterBase, companies))
			} else if role == "start_end_base" && matchedBase {
				if isCharterBase && len(companies) > 0 {
					baseHits++
				} else {
					failures = append(failures, fmt.Sprintf("start_end_base %q missing stay_details.is_charter_base=true or charter_companies", rm["name"]))
				}
			}
		}
		if matchedStop && (role == "overnight_stop" || role == "day_stop") {
			stopHits++
		}
	}

	domainChecks := 0
	totalDomain := 3
	if baseHits >= 1 {
		domainChecks++
	} else {
		failures = append(failures, fmt.Sprintf("expected at least 1 verified start_end_base matching %v with charter_companies", expectedBases))
	}
	if stopHits >= 2 {
		domainChecks++
	} else {
		failures = append(failures, fmt.Sprintf("expected at least 2 overnight_stop recommendations matching %v, got %d", expectedStops, stopHits))
	}
	if falsePositiveBases == 0 {
		domainChecks++
	}

	domainScore := (float64(domainChecks) / float64(totalDomain)) * 100.0
	return schemaScore, domainScore, failures
}

func scoreLookoutOutput(raw string, mode string) (float64, float64, []string) {
	cleaned := extractJSONPayload(raw)
	var alerts []map[string]any
	if err := json.Unmarshal([]byte(cleaned), &alerts); err != nil {
		return 0, 0, []string{fmt.Sprintf("invalid JSON array: %v", err)}
	}

	var failures []string
	validSchema := 0
	for _, a := range alerts {
		sev := fmt.Sprint(a["severity"])
		cat := fmt.Sprint(a["category"])
		msg := strings.TrimSpace(fmt.Sprint(a["message"]))
		act := strings.TrimSpace(fmt.Sprint(a["action"]))
		icon := strings.TrimSpace(fmt.Sprint(a["icon"]))
		validSev := sev == "danger" || sev == "warning" || sev == "info"
		validCat := cat == "weather" || cat == "tides" || cat == "navigation" || cat == "sun"
		if validSev && validCat && msg != "" && act != "" && icon != "" {
			validSchema++
		} else {
			failures = append(failures, fmt.Sprintf("malformed alert object: severity=%q category=%q", sev, cat))
		}
	}
	schemaScore := 100.0
	if len(alerts) > 0 {
		schemaScore = (float64(validSchema) / float64(len(alerts))) * 100.0
	}

	switch mode {
	case "heavy_weather":
		hasDanger := false
		for _, a := range alerts {
			if fmt.Sprint(a["severity"]) == "danger" {
				hasDanger = true
				break
			}
		}
		if !hasDanger {
			failures = append(failures, "expected at least one danger severity alert for 36-48 kt winds / 14 ft waves")
			return schemaScore, 0.0, failures
		}
		return schemaScore, 100.0, failures

	case "calm":
		for _, a := range alerts {
			sev := fmt.Sprint(a["severity"])
			cat := fmt.Sprint(a["category"])
			if (sev == "danger" || sev == "warning") && (cat == "weather" || cat == "tides" || cat == "navigation") {
				failures = append(failures, fmt.Sprintf("unexpected %s alert in calm conditions with no onward leg: %v", sev, a["message"]))
				return schemaScore, 0.0, failures
			}
		}
		return schemaScore, 100.0, failures

	case "hydrographic":
		hasNav := false
		for _, a := range alerts {
			if fmt.Sprint(a["category"]) == "navigation" {
				hasNav = true
				break
			}
		}
		if !hasNav {
			failures = append(failures, "expected at least one navigation alert for Cape Cod Canal transit")
			return schemaScore, 0.0, failures
		}
		return schemaScore, 100.0, failures

	case "adverse_wind":
		hasHeadwind := false
		for _, a := range alerts {
			cat := fmt.Sprint(a["category"])
			text := strings.ToLower(fmt.Sprint(a["message"]) + " " + fmt.Sprint(a["action"]))
			if cat == "navigation" && (strings.Contains(text, "wind") || strings.Contains(text, "beat") || strings.Contains(text, "head") || strings.Contains(text, "tack") || strings.Contains(text, "83") || strings.Contains(text, "85")) {
				hasHeadwind = true
				break
			}
		}
		if !hasHeadwind {
			failures = append(failures, "expected navigation alert identifying headwind/beating condition on 83 deg course")
			return schemaScore, 0.0, failures
		}
		return schemaScore, 100.0, failures

	case "planned_route":
		hasRouteAlert := false
		for _, a := range alerts {
			text := strings.ToLower(fmt.Sprint(a["message"]) + " " + fmt.Sprint(a["action"]))
			if strings.Contains(text, "solent") || strings.Contains(text, "segment") || strings.Contains(text, "270") || strings.Contains(text, "250") || strings.Contains(text, "headwind") || strings.Contains(text, "wind") {
				hasRouteAlert = true
				break
			}
		}
		if !hasRouteAlert {
			failures = append(failures, "expected alert referencing Solent Western Passage planned route segments or headwinds")
			return schemaScore, 0.0, failures
		}
		return schemaScore, 100.0, failures

	case "cascade":
		combined := strings.ToLower(cleaned)
		checks := 0
		if strings.Contains(combined, "tide") || strings.Contains(combined, "08:30") || strings.Contains(combined, "10:30") || strings.Contains(combined, "sill") {
			checks++
		} else {
			failures = append(failures, "cascade alert missing low-tide departure gate reference")
		}
		if strings.Contains(combined, "headwind") || strings.Contains(combined, "vmg") || strings.Contains(combined, "upwind") || strings.Contains(combined, "115") || strings.Contains(combined, "wind") {
			checks++
		} else {
			failures = append(failures, "cascade alert missing headwind/VMG slowdown reference")
		}
		if strings.Contains(combined, "sunset") || strings.Contains(combined, "night") || strings.Contains(combined, "unlit") || strings.Contains(combined, "dark") {
			checks++
		} else {
			failures = append(failures, "cascade alert missing sunset/night-entry unlit buoys reference")
		}
		return schemaScore, (float64(checks) / 3.0) * 100.0, failures
	}

	return schemaScore, 100.0, failures
}

func scorePilotOutput(raw string, expectBorder bool) (float64, float64, []string) {
	cleaned := extractJSONPayload(raw)
	var data map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err != nil {
		return 0, 0, []string{fmt.Sprintf("invalid JSON: %v", err)}
	}

	requiredKeys := []string{
		"summary",
		"sailing_season",
		"hazards",
		"security_safety",
		"hubs",
		"charter_info",
		"airports",
		"country_info",
		"currencies",
		"points_of_interest",
	}
	var failures []string
	present := 0
	for _, k := range requiredKeys {
		if data[k] != nil {
			present++
		} else {
			failures = append(failures, fmt.Sprintf("missing key %q", k))
		}
	}
	schemaScore := (float64(present) / float64(len(requiredKeys))) * 100.0

	domainChecks := 0
	totalDomain := 3
	if hubs, ok := data["hubs"].([]any); ok && len(hubs) >= 1 {
		domainChecks++
	} else {
		failures = append(failures, "expected at least 1 hub in pilot guide")
	}
	if airports, ok := data["airports"].([]any); ok && len(airports) >= 1 {
		domainChecks++
	} else {
		failures = append(failures, "expected at least 1 airport in pilot guide")
	}
	if charterInfo, ok := data["charter_info"].(map[string]any); ok {
		isCharter, _ := charterInfo["is_charter_destination"].(bool)
		companies, _ := charterInfo["companies"].([]any)
		if !isCharter || len(companies) > 0 {
			domainChecks++
		} else {
			failures = append(failures, "expected charter_info.companies to be non-empty when is_charter_destination=true")
		}
	} else {
		failures = append(failures, "expected charter_info object in pilot guide")
	}

	if expectBorder {
		totalDomain++
		lower := strings.ToLower(cleaned)
		if strings.Contains(lower, "canada") || strings.Contains(lower, "customs") || strings.Contains(lower, "cbsa") || strings.Contains(lower, "cbp") || strings.Contains(lower, "sidney") || strings.Contains(lower, "haro") {
			domainChecks++
		} else {
			failures = append(failures, "expected border/customs/Haro Strait pilotage details for San Juan Islands to Sidney, BC")
		}
	}

	return schemaScore, (float64(domainChecks) / float64(totalDomain)) * 100.0, failures
}

func isCanonicalSeedRegionName(name string) bool {
	nl := strings.ToLower(strings.TrimSpace(name))
	seedTokens := []string{
		"british virgin islands", "grenada", "grenadines", "sea of cortez",
		"san juan islands", "salish sea", "maine coast", "penobscot",
		"san francisco bay", "golden gate", "dalmatian", "croatia",
		"cyclades", "aegean", "andaman", "phuket", "whitsunday",
		"bay of islands", "angra dos reis", "ilha grande",
	}
	for _, st := range seedTokens {
		if strings.Contains(nl, st) {
			return true
		}
	}
	return false
}

func isCustomNonRectPolygon(ring []any) bool {
	if len(ring) >= 6 {
		return true
	}
	if len(ring) < 5 {
		return false
	}
	uniqueLats := make(map[string]bool)
	uniqueLngs := make(map[string]bool)
	for _, pt := range ring[:len(ring)-1] {
		if pair, ok := pt.([]any); ok && len(pair) >= 2 {
			lng, _ := toFloat64(pair[0])
			lat, _ := toFloat64(pair[1])
			uniqueLngs[fmt.Sprintf("%.2f", lng)] = true
			uniqueLats[fmt.Sprintf("%.2f", lat)] = true
		}
	}
	return len(uniqueLngs) > 2 || len(uniqueLats) > 2
}

func buildOutputSample(agentName, raw string) string {
	if agentName != agent.Commodore {
		return ""
	}
	cleaned := extractJSONPayload(raw)
	var regions []map[string]any
	if err := json.Unmarshal([]byte(cleaned), &regions); err != nil || len(regions) == 0 {
		return ""
	}
	var b strings.Builder
	novelCount := 0
	customPolyCount := 0
	var roster []string
	for _, r := range regions {
		name := strings.TrimSpace(fmt.Sprint(r["name"]))
		tier := strings.TrimSpace(fmt.Sprint(r["tier"]))
		originTag := "Seed"
		if !isCanonicalSeedRegionName(name) {
			originTag = "Novel"
			novelCount++
		}
		verts := 0
		if geom, ok := r["geometry"].(map[string]any); ok {
			if coords, ok := geom["coordinates"].([]any); ok && len(coords) > 0 {
				if ring, ok := coords[0].([]any); ok {
					verts = len(ring) - 1
					if isCustomNonRectPolygon(ring) {
						customPolyCount++
					}
				}
			}
		}
		roster = append(roster, fmt.Sprintf("**%s** (`%s` · `%s` · %d-pt poly)", name, tier, originTag, verts))
	}
	b.WriteString(fmt.Sprintf("- **Region Roster (%d total · %d Novel · %d Custom Polygons):** %s\n",
		len(regions), novelCount, customPolyCount, strings.Join(roster, ", ")))

	shown := 0
	for _, targetTier := range []string{"Classic Locale", "Hidden Gem", "Challenging", "Regional Favorite", "Standard"} {
		for _, r := range regions {
			if shown >= 4 {
				break
			}
			tier := strings.TrimSpace(fmt.Sprint(r["tier"]))
			if !strings.EqualFold(tier, targetTier) {
				continue
			}
			name := strings.TrimSpace(fmt.Sprint(r["name"]))
			summary := strings.TrimSpace(fmt.Sprint(r["summary"]))
			deepCut := strings.TrimSpace(fmt.Sprint(r["deep_cut_reasoning"]))
			if deepCut == "<nil>" {
				deepCut = ""
			}
			originTag := "Seed"
			if !isCanonicalSeedRegionName(name) {
				originTag = "Novel"
			}
			b.WriteString(fmt.Sprintf("- **%s [%s · %s]:** %s — *%s*\n", name, tier, originTag, summary, deepCut))
			shown++
		}
	}
	return strings.TrimSpace(b.String())
}

func scoreCommodoreOutput(raw string) (float64, float64, []string) {
	cleaned := extractJSONPayload(raw)
	var regions []map[string]any
	if err := json.Unmarshal([]byte(cleaned), &regions); err != nil {
		return 0, 0, []string{fmt.Sprintf("invalid JSON array: %v", err)}
	}

	var failures []string
	if len(regions) < 6 || len(regions) > 12 {
		failures = append(failures, fmt.Sprintf("expected 8-12 regions (min 6), got %d", len(regions)))
	}

	validSchema := 0
	tiers := make(map[string]bool)
	macroZones := make(map[string]bool)
	novelCount := 0
	customPolyCount := 0
	hasClassicLocale := false

	windRegimeKeywords := []string{
		"trade", "monsoon", "meltemi", "maestral", "mistral", "bora", "norther",
		"itcz", "azores high", "pacific high", "bermuda high", "subtropical", "ridge", "katabatic",
		"venturi", "sea breeze", "thermal", "roaring forties", "westerlies", "harmattan", "alisios", "papagayo",
	}
	quantMetricsKeywords := []string{
		"kt", "knot", "bf", "beaufort", "swell", "fetch", "sea state", "wave", "lee", "sheltered",
	}
	hydroCurrentKeywords := []string{
		"current", "tide", "tidal", "ebb", "flood", "spring", "neap", "rip", "race",
		"strait", "channel", "pass", "passage", "sill", "depth", "reef", "atoll", "fjord", "sound",
	}
	arbitrageKeywords := []string{
		"shoulder", "arbitrage", "hurricane", "cyclone", "typhoon", "storm", "belt",
		"latitude", "crowd", "solitude", "transition", "window", "equinox", "solstice", "peak", "summer", "winter", "regatta", "heritage",
	}

	dimHits := [4]int{}

	type bbox struct {
		name                 string
		minLng, minLat       float64
		maxLng, maxLat       float64
		centerLng, centerLat float64
	}
	var boxes []bbox

	for _, r := range regions {
		name := strings.TrimSpace(fmt.Sprint(r["name"]))
		tier := strings.TrimSpace(fmt.Sprint(r["tier"]))
		if strings.EqualFold(tier, "Classic Locale") || strings.EqualFold(tier, "Classic Sailing Locale") {
			tier = "Classic Locale"
			hasClassicLocale = true
		}
		summary := strings.TrimSpace(fmt.Sprint(r["summary"]))
		deepCut := strings.TrimSpace(fmt.Sprint(r["deep_cut_reasoning"]))
		if deepCut == "<nil>" {
			deepCut = ""
		}
		if !isCanonicalSeedRegionName(name) {
			novelCount++
		}

		geom, _ := r["geometry"].(map[string]any)
		geomType := fmt.Sprint(geom["type"])
		coords, _ := geom["coordinates"].([]any)

		polyOK := false
		var cLng, cLat float64
		if geomType == "Polygon" && len(coords) > 0 {
			if ring, ok := coords[0].([]any); ok && len(ring) >= 4 && len(ring) <= 9 {
				polyOK = true
				if isCustomNonRectPolygon(ring) {
					customPolyCount++
				}
				minLng, minLat := 180.0, 90.0
				maxLng, maxLat := -180.0, -90.0
				for _, pt := range ring {
					if pair, ok := pt.([]any); ok && len(pair) >= 2 {
						lng, _ := toFloat64(pair[0])
						lat, _ := toFloat64(pair[1])
						if lng < minLng {
							minLng = lng
						}
						if lng > maxLng {
							maxLng = lng
						}
						if lat < minLat {
							minLat = lat
						}
						if lat > maxLat {
							maxLat = lat
						}
					}
				}
				cLng = (minLng + maxLng) / 2
				cLat = (minLat + maxLat) / 2
				boxes = append(boxes, bbox{
					name:      name,
					minLng:    minLng,
					minLat:    minLat,
					maxLng:    maxLng,
					maxLat:    maxLat,
					centerLng: cLng,
					centerLat: cLat,
				})
				switch {
				case cLng < -30 && cLat >= 22:
					macroZones["North America / North Atlantic"] = true
				case cLng < -30 && cLat >= 8 && cLat < 22:
					macroZones["Caribbean / Tropics"] = true
				case cLng < -30 && cLat < 8:
					macroZones["South America"] = true
				case cLng >= -30 && cLng < 50:
					macroZones["Europe / Med / Atlantic / Africa"] = true
				case cLng >= 50 && cLat >= 0:
					macroZones["Asia / Northern Indian Ocean"] = true
				default:
					macroZones["Oceania / South Pacific / Southern Indian"] = true
				}
			}
		}
		if name != "" && tier != "" && summary != "" && deepCut != "" && polyOK {
			validSchema++
			tiers[tier] = true
		} else {
			failures = append(failures, fmt.Sprintf("region %q missing required schema fields (name/tier/summary/deep_cut_reasoning) or valid Polygon geometry", name))
		}

		combinedText := strings.ToLower(summary + " " + deepCut)
		for idx, kwList := range [][]string{windRegimeKeywords, quantMetricsKeywords, hydroCurrentKeywords, arbitrageKeywords} {
			for _, kw := range kwList {
				if strings.Contains(combinedText, kw) {
					dimHits[idx]++
					break
				}
			}
		}
	}

	schemaScore := 0.0
	if len(regions) > 0 {
		schemaScore = (float64(validSchema) / float64(len(regions))) * 100.0
	}

	overlaps := 0
	for i := 0; i < len(boxes); i++ {
		for j := i + 1; j < len(boxes); j++ {
			dLat := boxes[i].centerLat - boxes[j].centerLat
			dLng := boxes[i].centerLng - boxes[j].centerLng
			if math.Hypot(dLat, dLng) < 1.8 {
				overlaps++
				failures = append(failures, fmt.Sprintf("regions %q and %q have overlapping/adjacent centers (<1.8 deg apart)", boxes[i].name, boxes[j].name))
			}
		}
	}

	if len(tiers) < 3 {
		failures = append(failures, fmt.Sprintf("expected regions spanning at least 3 distinct tiers, got %d", len(tiers)))
	}
	if !hasClassicLocale {
		failures = append(failures, "expected at least 1 'Classic Locale' destination")
	}
	if len(macroZones) < 3 {
		failures = append(failures, fmt.Sprintf("expected global coverage across at least 3 macro-zones, got %d", len(macroZones)))
	}

	// Continuous 100-point Domain Accuracy breakdown:
	// 1. Structural, Tier & Spatial Coverage (30 pts):
	//    - 10 pts: 6-12 non-overlapping regions (>= 1.8 deg apart) + Classic Locale present
	//    - 10 pts: Tier diversity across the 5 tiers (2.0 pts per distinct tier, up to 10 pts)
	//    - 10 pts: Macro-zone diversity (2.5 pts per distinct macro-zone, up to 10 pts)
	// 2. Novel Synthesis & Custom Geometry (30 pts):
	//    - 20 pts: Novel regions outside the static seed (up to 3 novel regions -> full 20 pts)
	//    - 10 pts: Custom 5-8 vertex non-rectangular polygons (proportion of regions with custom polygons)
	// 3. Multi-Variable Synoptic & Hydrographic Reasoning Density (40 pts):
	//    - 10 pts each across 4 dimensions: Wind/Pressure Regime, Quantitative Wind/Sea-State,
	//      Hydrographic/Tidal/Current Mechanics, and Seasonal/Cyclone-Belt Arbitrage.
	domainScore := 0.0
	if len(regions) >= 6 && len(regions) <= 12 && overlaps == 0 && hasClassicLocale {
		domainScore += 10.0
	}
	tierPts := float64(len(tiers)) * 2.0
	if tierPts > 10.0 {
		tierPts = 10.0
	}
	domainScore += tierPts

	zonePts := float64(len(macroZones)) * 2.5
	if zonePts > 10.0 {
		zonePts = 10.0
	}
	domainScore += zonePts

	novelPts := (math.Min(float64(novelCount), 3.0) / 3.0) * 20.0
	domainScore += novelPts

	if len(regions) > 0 {
		polyPts := (float64(customPolyCount) / float64(len(regions))) * 10.0
		domainScore += polyPts
		for _, hits := range dimHits {
			domainScore += (float64(hits) / float64(len(regions))) * 10.0
		}
	}

	return schemaScore, math.Min(domainScore, 100.0), failures
}

func scoreDebriefOutput(raw string, forbiddenLiterals []string) (float64, float64, []string) {
	cleaned := extractJSONPayload(raw)
	var data map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err != nil {
		return 0, 0, []string{fmt.Sprintf("invalid JSON: %v", err)}
	}

	var failures []string
	stringFields := []string{"summary", "conclusions", "tacking_efficiency", "weather_impact"}
	schemaChecks := 0
	for _, f := range stringFields {
		if s, ok := data[f].(string); ok && strings.TrimSpace(s) != "" {
			schemaChecks++
		} else {
			failures = append(failures, fmt.Sprintf("missing or empty %q", f))
		}
	}
	obs, hasObs := data["observations"].([]any)
	if hasObs && len(obs) >= 1 {
		schemaChecks++
	} else {
		failures = append(failures, "missing non-empty observations array")
	}
	schemaScore := (float64(schemaChecks) / 5.0) * 100.0

	var allText strings.Builder
	for _, f := range stringFields {
		allText.WriteString(fmt.Sprint(data[f]))
		allText.WriteString(" ")
	}
	for _, o := range obs {
		allText.WriteString(fmt.Sprint(o))
		allText.WriteString(" ")
	}
	combined := allText.String()

	repeated := 0
	for _, lit := range forbiddenLiterals {
		if strings.Contains(combined, lit) {
			repeated++
			failures = append(failures, fmt.Sprintf("debrief repeated forbidden metric card literal %q", lit))
		}
	}

	domainScore := 100.0
	if repeated > 0 {
		domainScore = math.Max(0.0, 100.0-float64(repeated)*25.0)
	}
	return schemaScore, domainScore, failures
}

func scoreFacilityDiscoveryOutput(raw string, calls []ToolInvocation, areaMode bool) (float64, float64, []string) {
	cleaned := extractJSONPayload(raw)
	var data map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err != nil {
		return 0, 0, []string{fmt.Sprintf("invalid JSON: %v", err)}
	}

	facs, ok := data["facilities"].([]any)
	if !ok || len(facs) == 0 {
		return 0, 0, []string{"missing or empty facilities array"}
	}

	var failures []string
	validItems := 0
	for _, item := range facs {
		fm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(fm["name"]))
		typ := strings.TrimSpace(fmt.Sprint(fm["type"]))
		if name != "" && typ != "" && !strings.Contains(typ, "|") {
			validItems++
		}
	}
	schemaScore := (float64(validItems) / float64(len(facs))) * 100.0
	if validItems < len(facs) {
		failures = append(failures, fmt.Sprintf("%d/%d facilities had empty name/type or multi-pipe type", len(facs)-validItems, len(facs)))
	}

	domainChecks := 0
	totalDomain := 2
	if len(facs) >= 5 {
		domainChecks++
	} else {
		failures = append(failures, fmt.Sprintf("expected at least 5 facilities, got %d", len(facs)))
	}

	if areaMode {
		hasLargeRadius := false
		for _, c := range calls {
			if c.Name == "FindPlacesNearby" {
				if r, ok := toFloat64(c.Args["radius"]); ok && r >= 10000 {
					hasLargeRadius = true
					break
				}
			}
		}
		if hasLargeRadius {
			domainChecks++
		} else {
			failures = append(failures, "area mode must call FindPlacesNearby with radius >= 10000 meters")
		}
	} else {
		domainChecks++
	}

	return schemaScore, (float64(domainChecks) / float64(totalDomain)) * 100.0, failures
}

func scoreHydrographicPilotOutput(raw string) (float64, float64, []string) {
	cleaned := extractJSONPayload(raw)
	var data map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err != nil {
		return 0, 0, []string{fmt.Sprintf("invalid JSON: %v", err)}
	}

	var failures []string
	schemaChecks := 0
	if _, ok := data["data_available"].(bool); ok {
		schemaChecks++
	} else {
		failures = append(failures, "missing boolean data_available field")
	}
	if ov, ok := data["overview"].(string); ok && strings.TrimSpace(ov) != "" {
		schemaChecks++
	} else {
		failures = append(failures, "missing non-empty overview field")
	}
	if _, ok := data["hazards"].([]any); ok {
		schemaChecks++
	} else {
		failures = append(failures, "missing hazards array")
	}
	if _, ok := data["citations"].([]any); ok {
		schemaChecks++
	} else {
		failures = append(failures, "missing citations array")
	}
	schemaScore := (float64(schemaChecks) / 4.0) * 100.0

	avail, _ := data["data_available"].(bool)
	ov, _ := data["overview"].(string)
	domainScore := 0.0
	if avail {
		ac, _ := data["approach_and_channels"].(string)
		cits, _ := data["citations"].([]any)
		if strings.TrimSpace(ac) != "" || len(cits) > 0 {
			domainScore = 100.0
		} else {
			failures = append(failures, "data_available=true but approach_and_channels and citations are both empty")
			domainScore = 50.0
		}
	} else if strings.TrimSpace(ov) != "" {
		// Prompts explicitly allow data_available=false with an overview explanation when RAG corpus is unreachable.
		domainScore = 100.0
	} else {
		failures = append(failures, "hydrographic response missing overview explanation")
	}

	return schemaScore, domainScore, failures
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// allEvalCases returns the complete deterministic evaluation suite across all 8 agents.
func allEvalCases() []EvalCase {
	return []EvalCase{
		// --- HARBOURMASTER (3 cases) ---
		{
			AgentName:     agent.Harbourmaster,
			CaseID:        "oak_bluffs_research",
			Description:   "Oak Bluffs (41.4556, -70.5583) on July 15, 2025, radius 5 nm",
			Prompt:        "Research Oak Bluffs (lat: 41.4556, lng: -70.5583) on July 15, 2025. Radius 5 nm.",
			RequiredTools: []string{"GetWeather", "GetTides", "GetSunriseSunset", "facility_discovery", "hydrographic_pilot"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreHarbourmasterOutput(raw, false)
			},
		},
		{
			AgentName:     agent.Harbourmaster,
			CaseID:        "newport_ri_research",
			Description:   "Newport, RI (41.4901, -71.3128) on August 1, 2025, radius 10 nm",
			Prompt:        "Research Newport, RI (lat: 41.4901, lng: -71.3128) on August 1, 2025. Radius 10 nm.",
			RequiredTools: []string{"GetWeather", "GetTides", "GetSunriseSunset", "facility_discovery", "hydrographic_pilot"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreHarbourmasterOutput(raw, false)
			},
		},
		{
			AgentName:     agent.Harbourmaster,
			CaseID:        "cuttyhunk_to_block_island_tide_sunset_interaction",
			Description:   "Cuttyhunk to Block Island Great Salt Pond (29.8 NM, 241 deg) tide gate and sunset margin",
			Prompt:        "Research anchorages and weather for Cuttyhunk, MA (41.4244 N, 70.9317 W, Lat: 41.424400, Lng: -70.931700) for October 15, 2026. Radius 5 nm.\n\nNext destination: Block Island Great Salt Pond (41.1856, -71.5775) on October 15, 2026. Straight-line transit distance: 29.8 NM on course 241° (WSW). Call ComputePassageTimeline (passing both departure and arrival coordinates) and hydrographic_pilot for BOTH this departure harbor and the next destination to evaluate departure tide gates, arrival daylight margins, and night-entry hazards.",
			RequiredTools: []string{"GetWeather", "GetTides", "GetSunriseSunset", "ComputePassageTimeline", "hydrographic_pilot", "facility_discovery"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreHarbourmasterOutput(raw, true)
			},
		},

		// --- SPECIALIST (4 cases) ---
		{
			AgentName:     agent.Specialist,
			CaseID:        "nantucket_navigator",
			Description:   "Nantucket, MA (41.2835, -70.0995) resource hubs and anchorages within 10 nm",
			Prompt:        "Identify resource hubs and anchorages for 41.2835 N, 70.0995 W (Nantucket, MA). Radius 10 nm.",
			RequiredTools: []string{"facility_discovery", "hydrographic_pilot"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreSpecialistOutput(raw, nil, nil)
			},
		},
		{
			AgentName:     agent.Specialist,
			CaseID:        "day_trip_anchorages",
			Description:   "Nantucket, MA (41.2835, -70.0995) day-trip anchorages and lunch moorings within 15 nm",
			Prompt:        "Recommend 15 anchorages, moorings, and marinas within 15 nm of 41.2835°N, 70.0995°W (Nantucket, MA). This is a day trip — a single-day outing with no overnight stay. Prioritize day-use anchorages, lunch stops, and moorings suited to a few hours rather than overnight-only spots.",
			RequiredTools: []string{"facility_discovery"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreSpecialistOutput(raw, nil, nil)
			},
		},
		{
			AgentName:     agent.Specialist,
			CaseID:        "bvi_charter_base_vs_overnight_stop_classification",
			Description:   "Tortola BVI charter fleet bases (start_end_base) vs out-island stopovers (overnight_stop)",
			Prompt:        "Recommend anchorages, moorings, marinas, and fuel docks within 15 nm of center coordinates (latitude: 18.4207, longitude: -64.6399, 18.4207°N, 64.6399°W; center reference point near Tortola, British Virgin Islands). Classify each recommendation with voyage_role ('start_end_base', 'overnight_stop', 'day_stop', or 'service_only') and stay_details. Ensure Tortola charter fleet bases (Road Town / Wickhams Cay, Nanny Cay, and Hodges Creek / Parham Town / East End) are identified as 'start_end_base' with charter_companies populated, while out-island and resort stopover marinas (such as Scrub Island, Virgin Gorda, Norman Island, Peter Island, Cooper Island, and Soper's Hole) are classified as 'overnight_stop'.",
			RequiredTools: []string{"facility_discovery"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreSpecialistOutput(
					raw,
					[]string{"Road Town", "Wickham", "Nanny Cay", "Hodges Creek", "Parham", "East End", "Tortola", "Moorings", "Sunsail"},
					[]string{"Scrub", "Virgin Gorda", "Norman", "Peter", "Cooper", "Jost Van Dyke", "Soper", "Leverick", "Bitter End", "Cane Garden", "Trellis"},
				)
			},
		},
		{
			AgentName:     agent.Specialist,
			CaseID:        "chesapeake_charter_base_vs_overnight_stop_classification",
			Description:   "Chesapeake Bay Annapolis/Rock Hall charter bases vs St. Michaels/Galesville/Deale/Kent Island overnight stops",
			Prompt:        "Recommend anchorages, moorings, marinas, and fuel docks within 20 nm of center coordinates (latitude: 38.9784, longitude: -76.4922, 38.9784°N, 76.4922°W; center reference point near Annapolis, MD, Chesapeake Bay). Classify each recommendation with voyage_role ('start_end_base', 'overnight_stop', 'day_stop', or 'service_only') and stay_details. Ensure only verified commercial charter fleet bases (such as Annapolis / Eastport and Rock Hall) are classified as 'start_end_base', while overnight destination harbors and creek marinas (such as St. Michaels, Galesville, Deale, and Kent Island) are classified as 'overnight_stop'.",
			RequiredTools: []string{"facility_discovery"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreSpecialistOutput(
					raw,
					[]string{"Annapolis", "Eastport", "Rock Hall", "Back Creek", "Port Annapolis", "Haven Harbour"},
					[]string{"St. Michaels", "Saint Michaels", "Galesville", "Deale", "Kent Island", "Oxford", "Herring Bay", "Rhode River", "West River", "South River", "Wye", "Miles River", "Chester", "Whitehall", "Mill Creek", "Magothy", "Swan Creek", "Langford", "Corsica", "Poplar", "Tilghman", "Harness", "Selby", "Clements"},
				)
			},
		},
		{
			AgentName:     agent.Specialist,
			CaseID:        "buzzards_bay_newport_wide_circle_charter_bases",
			Description:   "36 NM circle centered at Dartmouth, MA (41.5294, -70.9597) finds Newport, RI and Padanaram charter bases across the circle",
			Prompt:        "Recommend anchorages, moorings, marinas, and fuel docks within 36 nm of center coordinates (latitude: 41.5294, longitude: -70.9597, 41.5294°N, 70.9597°W; center reference point near Dartmouth, Massachusetts, United States (including Newport)). IMPORTANT: \"Dartmouth, Massachusetts, United States (including Newport)\" is only the nearest point to the circle center — the search circle spans 36 nm in ALL directions (N, NE, E, SE, S, SW, W, NW) across multiple shores, harbors, and rivers. Identify every commercial yacht charter embarkation base across ALL major yachting harbors inside the search circle (including Newport, RI on the western side and Padanaram / South Dartmouth in Buzzards Bay) as voyage_role=\"start_end_base\" with stay_details.is_charter_base=true and stay_details.charter_companies populated, while offshore islands and stopover harbors (such as Block Island / Great Salt Pond / New Shoreham, Cuttyhunk, Martha's Vineyard, Hadley Harbor, Dutch Island, Jamestown, and Menemsha) are classified as 'overnight_stop' with is_charter_base=false.",
			RequiredTools: []string{"facility_discovery"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreSpecialistOutput(
					raw,
					[]string{"Newport", "Goat Island", "Bowen", "Bannister", "Padanaram", "South Wharf", "Dartmouth"},
					[]string{"Cuttyhunk", "Hadley", "Dutch Island", "Menemsha", "Tarpaulin", "Great Salt Pond", "Block Island", "New Shoreham", "Edgartown", "Vineyard Haven", "Oak Bluffs", "Marion", "Mattapoisett", "Jamestown", "Conanicut", "Kingman", "Red Brook", "Fairhaven", "Pope's Island", "Bristol"},
				)
			},
		},

		// --- LOOKOUT (6 cases) ---
		{
			AgentName:   agent.Lookout,
			CaseID:      "heavy_weather_danger_alert",
			Description: "Cuttyhunk to Block Island in 36-48 kt winds and 14 ft waves emits danger alert",
			Prompt:      "Audit safety for Stop 1 of 3: Position 41.42°N, -70.92°W (Cuttyhunk). Next Destination: Block Island (41.17°N, -71.56°W). Forecast winds 36 knots gusting to 48 knots, wave height 14 ft. Distance to next stop is 15 nm at course 220°.",
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreLookoutOutput(raw, "heavy_weather")
			},
		},
		{
			AgentName:   agent.Lookout,
			CaseID:      "calm_conditions_no_alerts",
			Description: "Final stop at Oak Bluffs in 8 kt winds and 1-2 ft waves emits no danger/warning alerts",
			Prompt:      "Audit safety for Stop 2 of 2: Position 41.38°N, -70.61°W (Oak Bluffs). Forecast wind 8 knots, waves 1-2 ft, tides normal, distance to next stop is none.",
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreLookoutOutput(raw, "calm")
			},
		},
		{
			AgentName:     agent.Lookout,
			CaseID:        "hydrographic_navigational_hazard_lookup",
			Description:   "Cape Cod Canal to Provincetown (12 nm at 060 deg) calls hydrographic_pilot and CalculateHeading",
			Prompt:        "Audit safety for Stop 1 of 2: Location: Cape Cod Canal (41.7400, -70.5700). Date: July 15, 2026. Next Destination: Provincetown (42.0500, -70.1800). Distance to next stop is 12 nm at course 060°. Weather is calm with wind 6 knots. Search sailing directions for local navigational hazards.",
			RequiredTools: []string{"hydrographic_pilot", "CalculateHeading"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreLookoutOutput(raw, "hydrographic")
			},
		},
		{
			AgentName:     agent.Lookout,
			CaseID:        "transit_heading_and_adverse_wind_check",
			Description:   "Cuttyhunk to Vineyard Haven on 83 deg course into 085 deg 18 kt headwind calls CalculateHeading",
			Prompt:        "Audit safety for Stop 1 of 2: Location: Cuttyhunk (41.4200, -70.9200). Date: 2026-08-01. Next Destination: Vineyard Haven (41.4500, -70.5800). Distance to next stop: 15.2 nautical miles at a course of 83° to Vineyard Haven. Forecast wind is from 085° at 18 knots.",
			RequiredTools: []string{"CalculateHeading"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreLookoutOutput(raw, "adverse_wind")
			},
		},
		{
			AgentName:   agent.Lookout,
			CaseID:      "planned_route_tailored_alerts",
			Description: "Cowes to Yarmouth via Solent Western Passage segments (250/270/220 deg) against 265 deg 20 kt wind",
			Prompt:      "Audit safety for Stop 1 of 2: Location: Cowes (50.7620, -1.2980). Date: 2026-08-10. Next Destination: Yarmouth (50.7050, -1.5000). Distance to next stop: 10.5 nautical miles via planned route \"Solent Western Passage\" (direct course 245° to Yarmouth).\n\nPlanned Route Details (\"Solent Western Passage\"):\n- Total Planned Distance: 10.5 NM\n- Key Route Segments & Course Bearings:\n  * Segment 1: 4.2 NM on course 250° (WSW) from (50.7620, -1.2980) to (50.7300, -1.3900)\n  * Segment 2: 3.8 NM on course 270° (W) from (50.7300, -1.3900) to (50.7100, -1.4800)\n  * Segment 3: 2.5 NM on course 220° (SW) from (50.7100, -1.4800) to (50.7050, -1.5000)\n\nForecast wind is from 265° at 20 knots. Search sailing directions for Solent western passage hazards and evaluate adverse winds along planned route segments.",
			AnyOfToolGroups: [][]string{
				{"CalculateHeading", "hydrographic_pilot"},
			},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreLookoutOutput(raw, "planned_route")
			},
		},
		{
			AgentName:   agent.Lookout,
			CaseID:      "tide_delayed_departure_night_arrival_cascade",
			Description: "Cuttyhunk Pond low-tide sill delay + 17 kt dead-ahead wind + Menemsha night entry unlit buoys cascade",
			Prompt:      "Audit safety for Stop 1 of 2: Location: Cuttyhunk Pond (41.4244, -70.9317). Date: October 20, 2026. Next Destination: Menemsha Basin (41.3536, -70.7667). Distance to next stop: 24.0 nautical miles at a course of 115° to Menemsha. Morning Low Tide at Cuttyhunk is at 08:30 AM (0.2 ft MLLW over a 6 ft controlling entrance channel sill, delaying safe departure until ~10:30 AM rising tide). Sunset is at 05:55 PM, and forecast wind is 17 knots from 115° (dead ahead, slowing VMG to ~3.8 knots so a 10:30 AM departure arrives after sunset around 04:50–06:15 PM). Coast Pilot notes unlit inner channel buoys and strong tidal rips at the Menemsha jetties. Evaluate the interaction between the low-tide departure delay, headwind transit speed, sunset arrival window, and night-entry aids.",
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreLookoutOutput(raw, "cascade")
			},
		},

		// --- PILOT (2 cases) ---
		{
			AgentName:     agent.Pilot,
			CaseID:        "british_virgin_islands_guide",
			Description:   "Regional sailing guide for Tortola, BVI (18.4286 N, 64.6176 W)",
			Prompt:        "Research the general sailing region for 18.4286 N, 64.6176 W (Tortola, BVI).",
			RequiredTools: []string{"FindPlacesNearby", "batch_google_search"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scorePilotOutput(raw, false)
			},
		},
		{
			AgentName:     agent.Pilot,
			CaseID:        "san_juan_islands_border_passage_guide",
			Description:   "Regional sailing guide for San Juan Islands, WA with US/Canada border passage to Sidney, BC",
			Prompt:        "Research the general sailing region for 48.5343 N, 123.0170 W (Friday Harbor, San Juan Islands, WA) with an intended passage route crossing Haro Strait and the US/Canada international border to Sidney, BC (48.6506 N, 123.3986 W).",
			RequiredTools: []string{"FindPlacesNearby", "batch_google_search"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scorePilotOutput(raw, true)
			},
		},
		{
			AgentName:     agent.Pilot,
			CaseID:        "buzzards_bay_newport_wide_circle_guide",
			Description:   "Regional sailing guide for 36 NM circle centered at Dartmouth, MA covering Newport, RI charter companies and hubs",
			Prompt:        "Research sailing guide for location: Dartmouth, Massachusetts, United States (including Newport) (Lat: 41.529371, Lng: -70.959654). Cover the entire cruising area within a 36 nm (~36 nm) radius of this center point, not just the immediate vicinity of the center location — explicitly include major yachting harbors and yacht charter companies across all sectors of the circle (including Dartmouth, Massachusetts, United States (including Newport)). The circle boundary reaches approximately: North edge (42.13°N, 70.96°W), South edge (40.93°N, 70.96°W), East edge (41.53°N, 70.16°W), West edge (41.53°N, 71.76°W). Include summary, sailing_season, hazards, hubs, charter_info, airports, country_info (including language, timezone, emergency numbers), currencies, and points_of_interest.",
			RequiredTools: []string{"FindPlacesNearby", "batch_google_search"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				s, d, f := scorePilotOutput(raw, false)
				lower := strings.ToLower(raw)
				if !strings.Contains(lower, "newport") {
					f = append(f, "expected wide-circle pilot guide to include Newport, RI hubs/charter operators")
					d = d * 0.5
				}
				return s, d, f
			},
		},

		// --- COMMODORE (3 cases) ---
		{
			AgentName:     agent.Commodore,
			CaseID:        "january_discovery",
			Description:   "Global seasonal sailing regions for January with RAG climatology and synoptic reasoning",
			Prompt:        "Identify prime sailing regions for the month of January.",
			RequiredTools: []string{"GetCandidateRegionsForMonth", "batch_google_search"},
			AnyOfToolGroups: [][]string{
				{"QuerySailingDirections", "QueryCoastPilot"},
			},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreCommodoreOutput(raw)
			},
		},
		{
			AgentName:     agent.Commodore,
			CaseID:        "july_mediterranean_discovery",
			Description:   "Global seasonal sailing regions for July with RAG climatology and synoptic reasoning",
			Prompt:        "Identify prime sailing regions for the month of July.",
			RequiredTools: []string{"GetCandidateRegionsForMonth", "batch_google_search"},
			AnyOfToolGroups: [][]string{
				{"QuerySailingDirections", "QueryCoastPilot"},
			},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreCommodoreOutput(raw)
			},
		},
		{
			AgentName:     agent.Commodore,
			CaseID:        "october_shoulder_season_arbitrage",
			Description:   "Global shoulder-season arbitrage and cyclone-belt transition discovery for October",
			Prompt:        "Identify prime sailing regions for the month of October, highlighting shoulder-season synoptic windows, low-latitude cyclone-belt refuges, and high-energy tidal/wind passages.",
			RequiredTools: []string{"GetCandidateRegionsForMonth", "batch_google_search"},
			AnyOfToolGroups: [][]string{
				{"QuerySailingDirections", "QueryCoastPilot"},
			},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreCommodoreOutput(raw)
			},
		},

		// --- DEBRIEF (2 cases) ---
		{
			AgentName:   agent.Debrief,
			CaseID:      "newport_block_island_debrief",
			Description: "Post-voyage tactical debrief for Newport to Block Island without repeating raw metric card numbers",
			Prompt:      "Perform a post-voyage tactical pilot debrief comparing the ACTUAL recorded passage against the PLANNED route:\n- Actual Track: 'Newport to Block Island (recorded)'\n- Paired Planned Route: 'Newport to Block Island (plan)'\n- Recorded Distance: 28.40 NM vs Planned Distance: 24.10 NM (Delta: +4.30 NM, +17.8% variance)\n- Recorded Duration: 5h 20m vs Planned Duration: 4h 00m (Delta: +1h 20m, +33.3% variance)\n- Vessel Speed: Average SOG 5.3 kts, Maximum SOG 7.1 kts\n\nGuidelines:\n- Be concise, direct, and tactical. Keep each section to 1-2 brief sentences.\n- DO NOT repeat raw numbers, percentages, or statistics already displayed in the metric cards.\n- Focus on actionable tactical reasons: weather, leeway, tacking choices, and sea state.\n\nOutput JSON matching:\n1. summary\n2. conclusions\n3. tacking_efficiency\n4. weather_impact\n5. observations",
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreDebriefOutput(raw, []string{"28.40", "24.10", "17.8%", "33.3%", "5.3", "7.1"})
			},
		},
		{
			AgentName:   agent.Debrief,
			CaseID:      "vineyard_sound_foul_tide_anomaly_debrief",
			Description: "Woods Hole to Edgartown debrief with timestamped SOG drop anomaly window and GPX telemetry",
			Prompt:      "Perform a post-voyage tactical pilot debrief comparing the ACTUAL recorded passage against the PLANNED route:\n- Actual Track: 'Woods Hole to Edgartown (recorded)'\n- Paired Planned Route: 'Woods Hole to Edgartown (plan)'\n- Recorded Distance: 18.60 NM vs Planned Distance: 14.20 NM (Delta: +4.40 NM, +31.0% variance)\n- Recorded Duration: 4h 15m vs Planned Duration: 2h 50m (Delta: +1h 25m, +50.0% variance)\n- Vessel Speed: Average SOG 4.4 kts, Maximum SOG 6.8 kts\n\nDeterministic GPX Telemetry Analysis:\n- Mean Cross-Track Error (XTE): 0.85 NM (max 1.42 NM south of rhumb line in Vineyard Sound)\n- Maneuvers: 6 tacks, mean tack angle 98°, net destination VMG 3.3 kts\n- Anomaly Window: Sudden SOG drop from 6.1 kts to 2.4 kts between 2026-07-18T14:00:00Z and 2026-07-18T15:15:00Z near Middle Ground (41.4800, -70.6000) coinciding with foul east-running flood current against SW headwind.\n\nGuidelines:\n- Be concise, direct, and tactical. Keep each section to 1-2 brief sentences.\n- DO NOT repeat raw numbers, percentages, or statistics already displayed in the metric cards (do not repeat 18.60, 14.20, 31.0%, 50.0%, 4.4, 6.8).\n- Ground tacking_efficiency, conclusions, and observations in the foul current and wide tacking angles.",
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreDebriefOutput(raw, []string{"18.60", "14.20", "31.0%", "50.0%", "4.4", "6.8"})
			},
		},

		// --- FACILITY_DISCOVERY (2 cases) ---
		{
			AgentName:     agent.FacilityDiscovery,
			CaseID:        "newport_ri_facilities",
			Description:   "Stop-mode facility discovery for Newport, RI (41.4901, -71.3128), radius 10 NM",
			Prompt:        "Discover marine facilities and anchorages for Newport, Rhode Island at latitude 41.4901, longitude -71.3128. Search Radius: 10 NM. Trip type: overnight.",
			RequiredTools: []string{"FindPlacesNearby", "batch_google_search"},
			ScoreFunc: func(raw string, calls []ToolInvocation) (float64, float64, []string) {
				return scoreFacilityDiscoveryOutput(raw, calls, false)
			},
		},
		{
			AgentName:     agent.FacilityDiscovery,
			CaseID:        "annapolis_area_mode_discovery",
			Description:   "Area-mode facility discovery for Annapolis, MD (38.9784, -76.4922), radius 20 NM",
			Prompt:        "Discover marine facilities and anchorages for Annapolis, Maryland at latitude 38.9784, longitude -76.4922. Search Radius: 20 NM. mode: \"area\". Trip type: overnight.",
			RequiredTools: []string{"FindPlacesNearby", "batch_google_search"},
			ScoreFunc: func(raw string, calls []ToolInvocation) (float64, float64, []string) {
				return scoreFacilityDiscoveryOutput(raw, calls, true)
			},
		},

		// --- HYDROGRAPHIC_PILOT (2 cases) ---
		{
			AgentName:   agent.HydrographicPilot,
			CaseID:      "cape_cod_canal_pilotage",
			Description: "US waters hydrographic pilotage for Cape Cod Canal to Plymouth (course 060 deg)",
			Prompt:      "Retrieve official hydrographic and pilotage intelligence for Cape Cod Canal (41.7400, -70.5700) in US waters. Next destination: Plymouth (41.9580, -70.6620), transit course 060°. Focus areas: channel depths, bridge clearances, currents, regulations.",
			AnyOfToolGroups: [][]string{
				{"QueryCoastPilot", "QuerySailingDirections"},
			},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreHydrographicPilotOutput(raw)
			},
		},
		{
			AgentName:     agent.HydrographicPilot,
			CaseID:        "solent_needles_channel_international_pilotage",
			Description:   "International waters hydrographic pilotage for Needles Channel & Western Solent, UK",
			Prompt:        "Retrieve official hydrographic and pilotage intelligence for The Solent and Needles Channel (50.6650, -1.5900) in UK international waters on a passage from Cowes (50.7620, -1.2980) to Yarmouth (50.7050, -1.5000). Focus areas: Needles Channel depths, Shingles Bank, Hurst Narrows tidal streams, hazards, and regulations.",
			RequiredTools: []string{"QuerySailingDirections"},
			ScoreFunc: func(raw string, _ []ToolInvocation) (float64, float64, []string) {
				return scoreHydrographicPilotOutput(raw)
			},
		},
	}
}

func parseCSVOrSpaceFilter(raw string) map[string]bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "all") {
		return nil
	}
	fields := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	})
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		out[strings.ToLower(strings.TrimSpace(f))] = true
	}
	return out
}

func buildSingleEvalAgent(ctx context.Context, baseCfg *config.Config, targetAgent, modelName string, recorder *evalToolRecorder, researcherTools []adktool.Tool) (adkagent.Agent, error) {
	caseCfg := *baseCfg
	caseCfg.SetTargetAgentModel(targetAgent, modelName)
	return agent.BuildSingle(ctx, &caseCfg, targetAgent, recorder.BeforeTool, nil, researcherTools)
}

func filterEvalCases(cases []EvalCase, agentsRaw, casesRaw string) []EvalCase {
	agentFilter := parseCSVOrSpaceFilter(agentsRaw)
	caseFilter := parseCSVOrSpaceFilter(casesRaw)
	if agentFilter == nil && caseFilter == nil {
		return cases
	}
	var filtered []EvalCase
	for _, c := range cases {
		if agentFilter != nil && !agentFilter[strings.ToLower(c.AgentName)] {
			continue
		}
		if caseFilter != nil && !caseFilter[strings.ToLower(c.CaseID)] {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

func isTransientEvalError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "503") ||
		strings.Contains(msg, "unavailable") ||
		strings.Contains(msg, "high demand") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "resource exhausted") ||
		strings.Contains(msg, "500") ||
		strings.Contains(msg, "internal error") ||
		strings.Contains(msg, "504") ||
		strings.Contains(msg, "deadline_exceeded") ||
		strings.Contains(msg, "overloaded")
}

func executeEvalCase(ctx context.Context, a adkagent.Agent, recorder *evalToolRecorder, tc EvalCase) CaseEvalResult {
	backoffs := []time.Duration{0, 3 * time.Second, 7 * time.Second}
	var lastRes CaseEvalResult

	for attempt, d := range backoffs {
		if d > 0 {
			time.Sleep(d)
		}
		recorder.Reset()
		sessionSvc := session.NewAutoCreate(adksession.InMemoryService())
		r, err := runner.New(runner.Config{
			AppName:           tc.AgentName,
			Agent:             a,
			SessionService:    sessionSvc,
			AutoCreateSession: true,
		})
		if err != nil {
			return evaluateCaseOutput(tc, "", nil, 0, fmt.Errorf("creating runner: %w", err))
		}

		caseCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		start := time.Now()
		sessionID := fmt.Sprintf("eval_%s_%s_%d_%d", tc.AgentName, tc.CaseID, start.UnixNano(), attempt)
		events := r.Run(caseCtx, "eval_user", sessionID, genai.NewContentFromText(tc.Prompt, "user"), adkagent.RunConfig{})

		var finalTurnText strings.Builder
		var allText strings.Builder
		var runErr error
		for event, err := range events {
			if err != nil {
				runErr = err
				break
			}
			if event != nil && event.LLMResponse.Content != nil {
				hasFunctionCall := false
				var turnBuf strings.Builder
				for _, part := range event.LLMResponse.Content.Parts {
					if part == nil {
						continue
					}
					if part.FunctionCall != nil {
						hasFunctionCall = true
					}
					if !part.Thought && part.Text != "" {
						turnBuf.WriteString(part.Text)
						allText.WriteString(part.Text)
					}
				}
				// Only non-tool-call model turns carry the final JSON response;
				// skip intermediate tool-calling chatter (e.g. "I will now query...").
				if !hasFunctionCall && turnBuf.Len() > 0 {
					finalTurnText.WriteString(turnBuf.String())
				}
			}
		}
		cancel()
		duration := time.Since(start)
		rawOutput := finalTurnText.String()
		if strings.TrimSpace(rawOutput) == "" {
			rawOutput = allText.String()
		}
		lastRes = evaluateCaseOutput(tc, rawOutput, recorder.Snapshot(), duration, runErr)
		if lastRes.Passed {
			return lastRes
		}
		if attempt < len(backoffs)-1 && isTransientEvalError(runErr) {
			continue
		}
		break
	}
	return lastRes
}

func TestEval(t *testing.T) {
	if !shouldRunEval("TestEval") {
		t.Skip("Skipping live agent evaluation (set RUN_EVAL=1 or -run TestEval to execute)")
	}

	loadEvalEnv()
	if err := prompt.Validate(); err != nil {
		t.Fatalf("Invalid prompts: %v", err)
	}

	cfg, err := config.New(os.Getenv)
	if err != nil {
		t.Fatalf("Failed to load config for eval: %v", err)
	}

	ctx := context.Background()
	nautical, providers, err := tool.NewNauticalService(
		ctx,
		cfg.MapsAPIKey,
		cfg.UKTidalAPIKey,
		cfg.NIWAAPIKey,
		cfg.Project,
		cfg.VertexLocation,
		cfg.CoastPilotCorpusID,
		cfg.NGACorpusID,
	)
	if err != nil {
		t.Fatalf("Failed to initialize nautical service: %v", err)
	}
	defer closeProviders(providers)

	researcherTools, err := nautical.AsTools()
	if err != nil {
		t.Fatalf("Failed to build researcher tools: %v", err)
	}

	selected := filterEvalCases(allEvalCases(), os.Getenv("AGENTS"), os.Getenv("CASES"))
	if len(selected) == 0 {
		t.Fatalf("No evaluation cases matched AGENTS=%q CASES=%q", os.Getenv("AGENTS"), os.Getenv("CASES"))
	}

	concurrency := evalModelConcurrency()
	sem := make(chan struct{}, concurrency)
	dispatchOrder := longestJobFirstIndices(selected)
	results := make([]CaseEvalResult, len(selected))
	var wg sync.WaitGroup
	var logMu sync.Mutex

	for _, idx := range dispatchOrder {
		idx := idx
		tc := selected[idx]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			recorder := &evalToolRecorder{}
			targetModel := cfg.ModelForAgent(tc.AgentName)
			a, err := buildSingleEvalAgent(ctx, cfg, tc.AgentName, targetModel, recorder, researcherTools)
			var res CaseEvalResult
			if err != nil {
				res = evaluateCaseOutput(tc, "", nil, 0, fmt.Errorf("building agent %s: %w", tc.AgentName, err))
			} else {
				res = executeEvalCase(ctx, a, recorder, tc)
			}
			results[idx] = res

			logMu.Lock()
			t.Logf("[%s/%s] pass=%v overall=%.1f%% (schema=%.1f%% tool=%.1f%% domain=%.1f%%) duration=%.1fs tools=%v",
				res.AgentName, res.CaseID, res.Passed, res.OverallScore, res.SchemaScore, res.ToolScore, res.DomainScore, res.Duration.Seconds(), res.ToolsCalled)
			logMu.Unlock()
		}()
	}
	wg.Wait()

	for _, res := range results {
		if !res.Passed {
			t.Errorf("Case %s/%s failed (overall=%.1f%%): %s",
				res.AgentName, res.CaseID, res.OverallScore, strings.Join(res.FailureReasons, "; "))
		}
	}
}
