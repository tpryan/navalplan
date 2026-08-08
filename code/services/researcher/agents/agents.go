// Package agents builds the fixed set of LLM agents that make up the
// researcher service (harbourmaster, pilot, commodore, specialist, lookout),
// plus the per-agent Google Search sub-agent each of them shares.
package agents

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tpryan/navalplan/services/researcher/config"
	"github.com/tpryan/navalplan/services/researcher/prompts"
	"github.com/tpryan/navalplan/services/researcher/sessions"
	"github.com/tpryan/navalplan/services/researcher/tools"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"
	"google.golang.org/adk/v2/tool/geminitool"
	"google.golang.org/genai"
)

const maxOutputTokens = 65536

// Names of the fixed set of agents this service exposes. Order matches the
// registration order used for HTTP routing and the multi-agent loader.
const (
	Harbourmaster = "harbourmaster"
	Pilot         = "pilot"
	Commodore     = "commodore"
	Specialist    = "specialist"
	Lookout       = "lookout"
)

// Order lists the agent names in the order they should be registered/loaded.
var Order = []string{Harbourmaster, Pilot, Commodore, Specialist, Lookout}

// spec describes how to build one of the fixed agents from shared building
// blocks (the nautical research tools, and a dedicated search sub-agent).
type spec struct {
	name              string
	description       string
	instruction       string
	temperature       float32
	includeResearcher bool // attach the shared MCP/nautical research tools
	includeSearch     bool // attach a dedicated Google-search sub-agent + batch tool
}

var specs = []spec{
	{
		name:              Pilot,
		description:       "A Local Knowledge Expert and Sailing Guide.",
		instruction:       prompts.Pilot,
		temperature:       0.25,
		includeResearcher: true,
		includeSearch:     true,
	},
	{
		name:              Harbourmaster,
		description:       "A Virtual Harbourmaster that researches sailing destinations.",
		instruction:       prompts.Harbourmaster,
		temperature:       0.25,
		includeResearcher: true,
		includeSearch:     true,
	},
	{
		name:              Commodore,
		description:       "The Commodore - Global Seasonal Discovery Expert.",
		instruction:       prompts.Commodore,
		temperature:       0.25,
		includeResearcher: false,
		includeSearch:     true,
	},
	{
		name:              Specialist,
		description:       "A Local Pilot and Navigation Specialist.",
		instruction:       prompts.Specialist,
		temperature:       0.25,
		includeResearcher: true,
		includeSearch:     true,
	},
	{
		name:              Lookout,
		description:       "A maritime safety auditor that analyzes stop data and returns structured safety alerts.",
		instruction:       prompts.Lookout,
		temperature:       0.1,
		includeResearcher: false,
		includeSearch:     false,
	},
}

// agentConfig is the internal shape passed to createAgent.
type agentConfig struct {
	name        string
	description string
	instruction string
	tools       []tool.Tool
	temperature float32
}

// Build constructs every agent described by specs, wiring researcherTools
// into the ones that request them, and instrumenting every tool call any of
// them makes via before/after. It returns the agents keyed by name (see the
// Harbourmaster/Pilot/Commodore/Specialist/Lookout constants and Order).
func Build(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, researcherTools []tool.Tool) (map[string]agent.Agent, error) {
	built := make(map[string]agent.Agent, len(specs))
	for _, sp := range specs {
		agentTools, err := toolsFor(ctx, cfg, before, after, sp, researcherTools)
		if err != nil {
			return nil, fmt.Errorf("assembling tools for %s: %w", sp.name, err)
		}

		a, err := createAgent(ctx, cfg, before, after, &agentConfig{
			name:        sp.name,
			description: sp.description,
			instruction: sp.instruction,
			tools:       agentTools,
			temperature: sp.temperature,
		})
		if err != nil {
			return nil, fmt.Errorf("creating %s agent: %w", sp.name, err)
		}
		built[sp.name] = a
	}
	return built, nil
}

// toolsFor assembles the tool list for a single agent spec. It always
// allocates a fresh slice (rather than appending onto researcherTools) so
// that agents built from the same shared researcherTools slice can never
// alias one another's backing array.
func toolsFor(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, sp spec, researcherTools []tool.Tool) ([]tool.Tool, error) {
	var agentTools []tool.Tool
	if sp.includeResearcher {
		agentTools = append(agentTools, researcherTools...)
	}
	if sp.includeSearch {
		searchTools, err := createSearchTools(ctx, cfg, before, after, sp.name+"_search_specialist")
		if err != nil {
			return nil, err
		}
		agentTools = append(agentTools, searchTools...)
	}
	return agentTools, nil
}

// createAgent builds a single llmagent from acfg, applying the shared model
// config, thinking-budget cap, and tool-call callbacks.
func createAgent(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, acfg *agentConfig) (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     genai.Ptr[float32](acfg.temperature),
	}

	// Cap reasoning tokens to keep latency bounded and protect the output budget.
	// A negative budget means "leave dynamic/default" (don't send a ThinkingConfig).
	if cfg.ThinkingBudget >= 0 {
		budget := cfg.ThinkingBudget
		genConfig.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: &budget}
	}

	m, err := gemini.NewModel(ctx, cfg.ModelName, &genai.ClientConfig{
		APIKey: cfg.GeminiAPIKey,
	})
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:                  acfg.name,
		Model:                 m,
		Description:           acfg.description,
		Instruction:           acfg.instruction,
		Tools:                 acfg.tools,
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{before},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{after},
		GenerateContentConfig: genConfig,
	})
}

// createSearchTools builds a dedicated Google-search sub-agent named name,
// then exposes it two ways: as a single agenttool (for one-off searches) and
// as a BatchSearchTool that fans a list of queries out to the same sub-agent
// in parallel.
func createSearchTools(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, name string) ([]tool.Tool, error) {
	searchAgent, err := createAgent(ctx, cfg, before, after, &agentConfig{
		name:        name,
		description: "Finds information on the web using Google Search.",
		instruction: prompts.SearchSpecialist,
		tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		temperature: 0.4,
	})
	if err != nil {
		return nil, err
	}

	individualTool := agenttool.New(searchAgent, nil)

	sessionSvc := sessions.NewAutoCreate(session.InMemoryService())

	// Create the batch tool that uses the search agent in parallel
	batchTool := &tools.BatchSearchTool{
		Searcher: func(ctx context.Context, query string) (string, error) {
			// Bound each grounded search so one slow query can't stall the whole
			// parallel batch (and therefore the entire agent turn).
			if cfg.SearchTimeoutMs > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.SearchTimeoutMs)*time.Millisecond)
				defer cancel()
			}

			// Create a runner for the search agent
			r, err := runner.New(runner.Config{
				AppName:        name,
				Agent:          searchAgent,
				SessionService: sessionSvc,
			})
			if err != nil {
				return "", err
			}

			resp := r.Run(ctx, "system", "batch_"+fmt.Sprint(time.Now().UnixNano()), genai.NewContentFromText(query, "user"), agent.RunConfig{})

			var text string
			for event, err := range resp {
				if err != nil {
					slog.Error("Search agent stream error", "query", query, "error", err)
					continue
				}
				if event.Content != nil && event.Content.Role == "model" {
					for _, part := range event.Content.Parts {
						if part.Text != "" && !part.Thought {
							text += part.Text
						}
					}
				}
			}

			if text == "" {
				slog.Warn("Search agent returned empty result", "query", query)
			} else {
				slog.Debug("Search agent result", "query", query, "text_len", len(text))
			}

			return text, nil
		},
	}

	return []tool.Tool{individualTool, batchTool}, nil
}
