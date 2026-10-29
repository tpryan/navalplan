package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tpryan/navalplan/services/researcher/internal/agent"
	"github.com/tpryan/navalplan/services/researcher/internal/config"
	"github.com/tpryan/navalplan/services/researcher/internal/prompt"
	"github.com/tpryan/navalplan/services/researcher/internal/tool"
)

const defaultComparisonModels = "gemini-3.5-flash-lite,gemini-4-argon,claude-haiku-4-5@20251001,claude-sonnet-4-5@20250929"

// ModelEvalSummary aggregates all case evaluation results and metrics for a single model.
type ModelEvalSummary struct {
	Model         string           `json:"model"`
	TotalCases    int              `json:"total_cases"`
	PassedCases   int              `json:"passed_cases"`
	PassRatePct   float64          `json:"pass_rate_pct"`
	AvgSchemaPct  float64          `json:"avg_schema_pct"`
	AvgToolPct    float64          `json:"avg_tool_pct"`
	AvgDomainPct  float64          `json:"avg_domain_pct"`
	AvgOverallPct float64          `json:"avg_overall_pct"`
	AvgLatency    time.Duration    `json:"avg_latency_ns"`
	TotalDuration time.Duration    `json:"total_duration_ns"`
	CaseResults   []CaseEvalResult `json:"case_results"`
}

func evalConcurrency() int {
	if v := strings.TrimSpace(os.Getenv("EVAL_CONCURRENCY")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 6
}

func evalModelConcurrency() int {
	if v := strings.TrimSpace(os.Getenv("EVAL_MODEL_CONCURRENCY")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 3
}

// evalAgentPriority assigns a dispatch priority (lower = dispatched first) so
// Longest-Job-First scheduling starts slow multi-tool agents at t=0 instead of
// letting them tail-straggle at the end of a run.
func evalAgentPriority(agentName string) int {
	switch agentName {
	case agent.FacilityDiscovery:
		return 1
	case agent.Specialist:
		return 2
	case agent.Harbourmaster:
		return 3
	case agent.Pilot:
		return 4
	case agent.Commodore:
		return 5
	case agent.Lookout:
		return 6
	case agent.HydrographicPilot:
		return 7
	case agent.Debrief:
		return 8
	default:
		return 9
	}
}

// longestJobFirstIndices returns indices [0..len(cases)-1] sorted so the
// longest-running agents are dispatched first while preserving original indices
// for deterministic result table ordering.
func longestJobFirstIndices(cases []EvalCase) []int {
	indices := make([]int, len(cases))
	for i := range cases {
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool {
		return evalAgentPriority(cases[indices[i]].AgentName) < evalAgentPriority(cases[indices[j]].AgentName)
	})
	return indices
}

func parseModelList(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = defaultComparisonModels
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	})
	var models []string
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		m := strings.TrimSpace(p)
		if m != "" && !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	return models
}

func summarizeModelResults(modelName string, results []CaseEvalResult, totalDuration time.Duration) ModelEvalSummary {
	summary := ModelEvalSummary{
		Model:         modelName,
		TotalCases:    len(results),
		TotalDuration: totalDuration,
		CaseResults:   results,
	}
	if len(results) == 0 {
		return summary
	}

	var sumSchema, sumTool, sumDomain, sumOverall float64
	var sumDuration time.Duration
	for _, r := range results {
		if r.Passed {
			summary.PassedCases++
		}
		sumSchema += r.SchemaScore
		sumTool += r.ToolScore
		sumDomain += r.DomainScore
		sumOverall += r.OverallScore
		sumDuration += r.Duration
	}

	n := float64(len(results))
	summary.PassRatePct = (float64(summary.PassedCases) / n) * 100.0
	summary.AvgSchemaPct = sumSchema / n
	summary.AvgToolPct = sumTool / n
	summary.AvgDomainPct = sumDomain / n
	summary.AvgOverallPct = sumOverall / n
	summary.AvgLatency = time.Duration(int64(sumDuration) / int64(len(results)))
	if summary.TotalDuration == 0 {
		summary.TotalDuration = sumDuration
	}
	return summary
}

func formatComparisonScorecard(summaries []ModelEvalSummary) string {
	var b strings.Builder

	// 1. ASCII Banner Table
	sep := strings.Repeat("=", 118)
	dash := strings.Repeat("-", 118)
	b.WriteString("\n" + sep + "\n")
	b.WriteString("                              NAVALPLAN MULTI-MODEL EVALUATION SCORECARD\n")
	b.WriteString(sep + "\n")
	b.WriteString(fmt.Sprintf("%-26s | %-7s | %-9s | %-8s | %-10s | %-12s | %-9s | %-11s | %-14s\n",
		"Model", "Cases", "Pass Rate", "Schema %", "Tool Use %", "Domain Acc %", "Overall %", "Avg Latency", "Total Duration"))
	b.WriteString(dash + "\n")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf("%-26s | %2d/%-4d | %8.1f%% | %7.1f%% | %9.1f%% | %11.1f%% | %8.1f%% | %10.1fs | %13.1fs\n",
			s.Model, s.PassedCases, s.TotalCases, s.PassRatePct, s.AvgSchemaPct, s.AvgToolPct, s.AvgDomainPct, s.AvgOverallPct,
			s.AvgLatency.Seconds(), s.TotalDuration.Seconds()))
	}
	b.WriteString(sep + "\n\n")

	// 2. Markdown Model Evaluation Comparison Matrix
	b.WriteString("## Model Evaluation Comparison Matrix\n\n")
	b.WriteString("| Model | Cases | Pass Rate | Schema % | Tool Use % | Domain Acc % | Overall % | Avg Latency | Total Duration |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf("| `%s` | %d/%d | %.1f%% | %.1f%% | %.1f%% | %.1f%% | %.1f%% | %.1fs | %.1fs |\n",
			s.Model, s.PassedCases, s.TotalCases, s.PassRatePct, s.AvgSchemaPct, s.AvgToolPct, s.AvgDomainPct, s.AvgOverallPct,
			s.AvgLatency.Seconds(), s.TotalDuration.Seconds()))
	}
	b.WriteString("\n")

	// Collect ordered agents and cases present across summaries.
	var agentsPresent []string
	agentSeen := make(map[string]bool)
	for _, canonical := range agent.EvalOrder {
		for _, s := range summaries {
			for _, cr := range s.CaseResults {
				if cr.AgentName == canonical && !agentSeen[canonical] {
					agentSeen[canonical] = true
					agentsPresent = append(agentsPresent, canonical)
				}
			}
		}
	}

	// 3. Markdown Per-Agent Breakdown by Model
	b.WriteString("## Per-Agent Breakdown by Model\n\n")
	b.WriteString("| Agent | Cases |")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf(" `%s` (Pass / Score / Time) |", s.Model))
	}
	if len(summaries) >= 2 {
		b.WriteString(" Fastest (x) |")
	}
	b.WriteString("\n|---|---|")
	for range summaries {
		b.WriteString("---|")
	}
	if len(summaries) >= 2 {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	for _, ag := range agentsPresent {
		caseCount := 0
		var modelDurations []struct {
			model string
			sec   float64
		}
		var cells []string
		for _, s := range summaries {
			passed, total := 0, 0
			var scoreSum float64
			var durSum time.Duration
			for _, cr := range s.CaseResults {
				if cr.AgentName == ag {
					total++
					if cr.Passed {
						passed++
					}
					scoreSum += cr.OverallScore
					durSum += cr.Duration
				}
			}
			if total > caseCount {
				caseCount = total
			}
			if total == 0 {
				cells = append(cells, " - ")
				continue
			}
			avgScore := scoreSum / float64(total)
			sec := durSum.Seconds()
			if passed > 0 {
				modelDurations = append(modelDurations, struct {
					model string
					sec   float64
				}{model: s.Model, sec: sec})
			}
			cells = append(cells, fmt.Sprintf(" %d/%d (%.1f%%, %.1fs) ", passed, total, avgScore, sec))
		}
		b.WriteString(fmt.Sprintf("| `%s` | %d |%s", ag, caseCount, strings.Join(cells, "|")))
		if len(summaries) >= 2 {
			b.WriteString("| " + formatFastestColumn(modelDurations) + " |")
		} else {
			b.WriteString("|")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// 4. Markdown Per-Case Side-by-Side Breakdown
	b.WriteString("## Per-Case Side-by-Side Breakdown\n\n")
	b.WriteString("| # | Agent | Test Case |")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf(" `%s` |", s.Model))
	}
	b.WriteString("\n|---|---|---|")
	for range summaries {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	type caseKey struct {
		agentName string
		caseID    string
	}
	var orderedCases []caseKey
	caseSeen := make(map[caseKey]bool)
	for _, s := range summaries {
		for _, cr := range s.CaseResults {
			ck := caseKey{agentName: cr.AgentName, caseID: cr.CaseID}
			if !caseSeen[ck] {
				caseSeen[ck] = true
				orderedCases = append(orderedCases, ck)
			}
		}
	}

	for idx, ck := range orderedCases {
		b.WriteString(fmt.Sprintf("| %d | `%s` | `%s` |", idx+1, ck.agentName, ck.caseID))
		for _, s := range summaries {
			var found *CaseEvalResult
			for i := range s.CaseResults {
				if s.CaseResults[i].AgentName == ck.agentName && s.CaseResults[i].CaseID == ck.caseID {
					found = &s.CaseResults[i]
					break
				}
			}
			if found == nil {
				b.WriteString(" - |")
				continue
			}
			status := "PASS"
			if !found.Passed {
				status = "FAIL"
			}
			b.WriteString(fmt.Sprintf(" %s %.1f%% (S:%.0f T:%.0f D:%.0f, %.1fs) |",
				status, found.OverallScore, found.SchemaScore, found.ToolScore, found.DomainScore, found.Duration.Seconds()))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// 5. Per-Case Failure & Diagnostic Diffs
	b.WriteString("## Per-Case Failure & Diagnostic Diffs\n\n")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf("#### %s (%d/%d passed)\n", s.Model, s.PassedCases, s.TotalCases))
		failuresCount := 0
		for _, cr := range s.CaseResults {
			if !cr.Passed || len(cr.FailureReasons) > 0 {
				failuresCount++
				b.WriteString(fmt.Sprintf("- **`%s/%s`** (overall=%.1f%%, schema=%.1f%%, tool=%.1f%%, domain=%.1f%%, tools=%v): %s\n",
					cr.AgentName, cr.CaseID, cr.OverallScore, cr.SchemaScore, cr.ToolScore, cr.DomainScore,
					cr.ToolsCalled, strings.Join(cr.FailureReasons, "; ")))
			}
		}
		if failuresCount == 0 {
			b.WriteString("- All cases passed with no diagnostic failures.\n")
		}
		b.WriteString("\n")
	}

	// 6. Sampled Output Quality & Reasoning Comparison
	hasSamples := false
	for _, s := range summaries {
		for _, cr := range s.CaseResults {
			if strings.TrimSpace(cr.OutputSample) != "" {
				hasSamples = true
				break
			}
		}
	}
	if hasSamples {
		b.WriteString("## Sampled Output Quality & Reasoning Comparison\n\n")
		for _, ck := range orderedCases {
			anyCaseSample := false
			for _, s := range summaries {
				for _, cr := range s.CaseResults {
					if cr.AgentName == ck.agentName && cr.CaseID == ck.caseID && strings.TrimSpace(cr.OutputSample) != "" {
						anyCaseSample = true
						break
					}
				}
			}
			if !anyCaseSample {
				continue
			}
			b.WriteString(fmt.Sprintf("### `%s/%s`\n\n", ck.agentName, ck.caseID))
			for _, s := range summaries {
				for _, cr := range s.CaseResults {
					if cr.AgentName == ck.agentName && cr.CaseID == ck.caseID && strings.TrimSpace(cr.OutputSample) != "" {
						b.WriteString(fmt.Sprintf("#### `%s` (Overall %.1f%% · Domain %.1f%% · %.1fs)\n%s\n\n",
							s.Model, cr.OverallScore, cr.DomainScore, cr.Duration.Seconds(), cr.OutputSample))
					}
				}
			}
		}
	}

	return b.String()
}

func formatFastestColumn(entries []struct {
	model string
	sec   float64
}) string {
	if len(entries) < 2 {
		return "-"
	}
	fastest := entries[0]
	slowest := entries[0]
	for _, e := range entries[1:] {
		if e.sec < fastest.sec {
			fastest = e
		}
		if e.sec > slowest.sec {
			slowest = e
		}
	}
	if fastest.sec <= 0 {
		return fmt.Sprintf("`%s`", fastest.model)
	}
	ratio := slowest.sec / fastest.sec
	return fmt.Sprintf("`%s` (%.2fx)", fastest.model, ratio)
}

func printComparisonScorecard(summaries []ModelEvalSummary) string {
	report := formatComparisonScorecard(summaries)
	fmt.Print(report)
	return report
}

func writeComparisonArtifacts(summaries []ModelEvalSummary, markdownReport string) (string, string, error) {
	outDir := filepath.Join("eval", "results")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
	}
	stamp := time.Now().Format("20060102_150405")
	mdPath := filepath.Join(outDir, fmt.Sprintf("compare_%s.md", stamp))
	jsonPath := filepath.Join(outDir, fmt.Sprintf("compare_%s.json", stamp))

	if err := os.WriteFile(mdPath, []byte(markdownReport), 0o644); err != nil {
		return "", "", err
	}
	payload := map[string]any{
		"timestamp": stamp,
		"summaries": summaries,
	}
	rawJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return mdPath, "", err
	}
	if err := os.WriteFile(jsonPath, rawJSON, 0o644); err != nil {
		return mdPath, "", err
	}
	return mdPath, jsonPath, nil
}

func TestEval_ModelComparison(t *testing.T) {
	if !shouldRunEval("TestEval_ModelComparison") {
		t.Skip("Skipping multi-model comparison (set RUN_EVAL=1 or -run TestEval_ModelComparison to execute)")
	}

	loadEvalEnv()
	if err := prompt.Validate(); err != nil {
		t.Fatalf("Invalid prompts: %v", err)
	}

	baseCfg, err := config.New(os.Getenv)
	if err != nil {
		t.Fatalf("Failed to load config for model comparison: %v", err)
	}

	models := parseModelList(os.Getenv("MODELS"))
	selected := filterEvalCases(allEvalCases(), os.Getenv("AGENTS"), os.Getenv("CASES"))
	if len(selected) == 0 {
		t.Fatalf("No evaluation cases matched AGENTS=%q CASES=%q", os.Getenv("AGENTS"), os.Getenv("CASES"))
	}

	ctx := context.Background()
	nautical, providers, err := tool.NewNauticalService(
		ctx,
		baseCfg.MapsAPIKey,
		baseCfg.UKTidalAPIKey,
		baseCfg.NIWAAPIKey,
		baseCfg.Project,
		baseCfg.VertexLocation,
		baseCfg.CoastPilotCorpusID,
		baseCfg.NGACorpusID,
	)
	if err != nil {
		t.Fatalf("Failed to initialize nautical service: %v", err)
	}
	defer closeProviders(providers)

	researcherTools, err := nautical.AsTools()
	if err != nil {
		t.Fatalf("Failed to build researcher tools: %v", err)
	}

	globalConcurrency := evalConcurrency()
	perModelConcurrency := evalModelConcurrency()
	dispatchOrder := longestJobFirstIndices(selected)
	t.Logf("=== Running %d models x %d cases in parallel (global_concurrency=%d, per_model_concurrency=%d, Longest-Job-First) ===",
		len(models), len(selected), globalConcurrency, perModelConcurrency)

	globalSem := make(chan struct{}, globalConcurrency)
	var logMu sync.Mutex

	summaries := make([]ModelEvalSummary, len(models))
	var modelWG sync.WaitGroup

	for mIdx, modelName := range models {
		mIdx, modelName := mIdx, modelName
		modelWG.Add(1)
		go func() {
			defer modelWG.Done()
			modelStart := time.Now()
			results := make([]CaseEvalResult, len(selected))
			modelSem := make(chan struct{}, perModelConcurrency)
			var caseWG sync.WaitGroup

			for _, idx := range dispatchOrder {
				idx := idx
				tc := selected[idx]
				caseWG.Add(1)
				go func() {
					defer caseWG.Done()
					modelSem <- struct{}{}
					defer func() { <-modelSem }()
					globalSem <- struct{}{}
					defer func() { <-globalSem }()

					recorder := &evalToolRecorder{}
					a, err := buildSingleEvalAgent(ctx, baseCfg, tc.AgentName, modelName, recorder, researcherTools)
					var res CaseEvalResult
					if err != nil {
						res = evaluateCaseOutput(tc, "", nil, 0, fmt.Errorf("building agent %s for model %s: %w", tc.AgentName, modelName, err))
					} else {
						res = executeEvalCase(ctx, a, recorder, tc)
					}
					results[idx] = res

					logMu.Lock()
					t.Logf("[model=%s] [%s/%s] pass=%v overall=%.1f%% (schema=%.1f%% tool=%.1f%% domain=%.1f%%) duration=%.1fs",
						modelName, res.AgentName, res.CaseID, res.Passed, res.OverallScore, res.SchemaScore, res.ToolScore, res.DomainScore, res.Duration.Seconds())
					logMu.Unlock()
				}()
			}
			caseWG.Wait()
			summaries[mIdx] = summarizeModelResults(modelName, results, time.Since(modelStart))
		}()
	}
	modelWG.Wait()

	report := printComparisonScorecard(summaries)
	if mdPath, jsonPath, err := writeComparisonArtifacts(summaries, report); err != nil {
		t.Logf("Warning: failed to write comparison artifacts: %v", err)
	} else {
		t.Logf("Wrote comparison artifacts:\n  %s\n  %s", mdPath, jsonPath)
	}
}
