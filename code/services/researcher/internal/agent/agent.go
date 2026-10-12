// Package agent builds the fixed set of LLM agents that make up the
// researcher service (harbourmaster, pilot, commodore, specialist, lookout),
// plus the per-agent Google Search sub-agent each of them shares.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/tpryan/navalplan/services/researcher/internal/config"
	"github.com/tpryan/navalplan/services/researcher/internal/prompt"
	"github.com/tpryan/navalplan/services/researcher/internal/session"
	"github.com/tpryan/navalplan/services/researcher/internal/tool"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	adktool "google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"
	"google.golang.org/adk/v2/tool/geminitool"
	"google.golang.org/genai"
)

const maxOutputTokens = 8192

const (
	Harbourmaster = "harbourmaster"
	Pilot         = "pilot"
	Commodore     = "commodore"
	Specialist    = "specialist"
	Lookout       = "lookout"
)

// Nautical tool names, exactly as registered in
// tool.NauticalToolService.AsTools(). Agents opt into tools by name via
// spec.toolNames, so each agent receives only the tools its task needs
// instead of the full "kitchen-sink" bundle.
const (
	toolGetTides               = "GetTides"
	toolGetWeather             = "GetWeather"
	toolGetSunriseSunset       = "GetSunriseSunset"
	toolFindPlacesNearby       = "FindPlacesNearby"
	toolGetSafetyAlerts        = "GetSafetyAlerts"
	toolQuerySailingDirections = "QuerySailingDirections"
	toolQueryCoastPilot        = "QueryCoastPilot"
	toolCalculateHeading       = "CalculateHeading"
)

// allNauticalTools is the full briefing-grade bundle (environment, facilities,
// safety, pilotage, and heading geometry) shared by the destination-research
// agents. FilterGPXData is intentionally excluded: GPX smoothing runs
// deterministically in Go on upload, not via an LLM tool call.
var allNauticalTools = []string{
	toolGetTides,
	toolGetWeather,
	toolGetSunriseSunset,
	toolFindPlacesNearby,
	toolGetSafetyAlerts,
	toolQuerySailingDirections,
	toolQueryCoastPilot,
	toolCalculateHeading,
}

var Order = []string{Harbourmaster, Pilot, Commodore, Specialist, Lookout}

type spec struct {
	name          string
	description   string
	instruction   string
	temperature   float32
	toolNames     []string
	includeSearch bool
}

var specs = []spec{
	{
		name:          Pilot,
		description:   "A Local Knowledge Expert and Sailing Guide.",
		instruction:   prompt.Pilot,
		temperature:   0.25,
		toolNames:     allNauticalTools,
		includeSearch: true,
	},
	{
		name:          Harbourmaster,
		description:   "A Virtual Harbourmaster that researches sailing destinations.",
		instruction:   prompt.Harbourmaster,
		temperature:   0.25,
		toolNames:     allNauticalTools,
		includeSearch: true,
	},
	{
		// Commodore performs global seasonal discovery from the open web only;
		// it needs no station-level nautical tools.
		name:          Commodore,
		description:   "The Commodore - Global Seasonal Discovery Expert.",
		instruction:   prompt.Commodore,
		temperature:   0.25,
		toolNames:     nil,
		includeSearch: true,
	},
	{
		// Specialist recommends anchorages and facilities: places discovery
		// plus hydrographic pilotage. No weather/tides/sun.
		name:          Specialist,
		description:   "A Local Pilot and Navigation Specialist.",
		instruction:   prompt.Specialist,
		temperature:   0.25,
		toolNames:     []string{toolFindPlacesNearby, toolQuerySailingDirections, toolQueryCoastPilot},
		includeSearch: true,
	},
	{
		// Lookout audits safety: environment, pilotage, heading geometry, and
		// navigational warnings. No places discovery and no web search.
		name:        Lookout,
		description: "A maritime safety auditor that analyzes stop data and returns structured safety alerts.",
		instruction: prompt.Lookout,
		temperature: 0.1,
		toolNames: []string{
			toolGetTides,
			toolGetWeather,
			toolGetSunriseSunset,
			toolQuerySailingDirections,
			toolQueryCoastPilot,
			toolCalculateHeading,
			toolGetSafetyAlerts,
		},
		includeSearch: false,
	},
}

type agentConfig struct {
	name        string
	description string
	instruction string
	tools       []adktool.Tool
	temperature float32
}

func Build(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, researcherTools []adktool.Tool) (map[string]agent.Agent, error) {
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

func toolsFor(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, sp spec, researcherTools []adktool.Tool) ([]adktool.Tool, error) {
	var agentTools []adktool.Tool
	if len(sp.toolNames) > 0 {
		byName := make(map[string]adktool.Tool, len(researcherTools))
		for _, t := range researcherTools {
			byName[t.Name()] = t
		}
		for _, name := range sp.toolNames {
			t, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("tool %q requested by agent %q is not registered", name, sp.name)
			}
			agentTools = append(agentTools, t)
		}
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

func createAgent(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, acfg *agentConfig) (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     genai.Ptr[float32](acfg.temperature),
	}

	if cfg.ThinkingBudget >= 0 {
		budget := cfg.ThinkingBudget
		genConfig.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: &budget}
	}

	var clientCfg *genai.ClientConfig
	if os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "true" || os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "1" {
		loc := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if loc == "" {
			loc = "global"
		}
		clientCfg = &genai.ClientConfig{
			Project:  cfg.Project,
			Location: loc,
			Backend:  genai.BackendVertexAI,
		}
	} else {
		clientCfg = &genai.ClientConfig{
			APIKey: cfg.GeminiAPIKey,
		}
	}
	m, err := gemini.NewModel(ctx, cfg.ModelName, clientCfg)
	if err != nil {
		return nil, err
	}

	var beforeCallbacks []llmagent.BeforeToolCallback
	if before != nil {
		beforeCallbacks = []llmagent.BeforeToolCallback{before}
	}
	var afterCallbacks []llmagent.AfterToolCallback
	if after != nil {
		afterCallbacks = []llmagent.AfterToolCallback{after}
	}

	return llmagent.New(llmagent.Config{
		Name:                  acfg.name,
		Model:                 m,
		Description:           acfg.description,
		Instruction:           acfg.instruction,
		Tools:                 acfg.tools,
		GenerateContentConfig: genConfig,
		BeforeToolCallbacks:   beforeCallbacks,
		AfterToolCallbacks:    afterCallbacks,
	})
}

func createSearchTools(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, name string) ([]adktool.Tool, error) {
	searchAgent, err := createAgent(ctx, cfg, before, after, &agentConfig{
		name:        name,
		description: "Finds information on the web using Google Search.",
		instruction: prompt.SearchSpecialist,
		tools: []adktool.Tool{
			geminitool.GoogleSearch{},
		},
		temperature: 0.4,
	})
	if err != nil {
		return nil, err
	}

	individualTool := agenttool.New(searchAgent, nil)
	sessionSvc := session.NewAutoCreate(adksession.InMemoryService())

	batchTool := &tool.BatchSearchTool{
		Searcher: func(ctx context.Context, query string) (string, error) {
			if cfg.SearchTimeoutMs > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.SearchTimeoutMs)*time.Millisecond)
				defer cancel()
			}

			r, err := runner.New(runner.Config{
				AppName:           name,
				Agent:             searchAgent,
				SessionService:    sessionSvc,
				AutoCreateSession: true,
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

	return []adktool.Tool{individualTool, batchTool}, nil
}
