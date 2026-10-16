// Package agent builds the fixed set of LLM agents that make up the
// researcher service (harbourmaster, pilot, commodore, specialist, lookout),
// plus the per-agent Google Search sub-agent each of them shares.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

// maxOutputTokens is the model maximum. The harbourmaster returns the whole
// briefing (weather, tides, pilot notes, every facility) as one JSON object;
// at 8192 dense stops were truncated mid-JSON and saved as empty briefings.
const maxOutputTokens = 65536

const (
	Harbourmaster = "harbourmaster"
	Pilot         = "pilot"
	Debrief       = "debrief"
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

var Order = []string{Harbourmaster, Pilot, Debrief, Commodore, Specialist, Lookout}

type spec struct {
	name                     string
	description              string
	instruction              string
	temperature              float32
	toolNames                []string
	includeSearch            bool
	includeFacilityDiscovery bool
	includeHydrographicPilot bool
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
		// Debrief performs a post-voyage tactical review of a recorded track
		// against its planned route. All telemetry is supplied directly in the
		// prompt, so it needs no tools and no web search.
		name:          Debrief,
		description:   "A post-voyage tactical pilot that debriefs a recorded passage against its planned route.",
		instruction:   prompt.Debrief,
		temperature:   0.25,
		toolNames:     nil,
		includeSearch: false,
	},
	{
		// Harbourmaster delegates facility/anchorage discovery to the
		// facility_discovery sub-agent, so it no longer holds FindPlacesNearby
		// directly.
		name:        Harbourmaster,
		description: "A Virtual Harbourmaster that researches sailing destinations.",
		instruction: prompt.Harbourmaster,
		temperature: 0.25,
		toolNames: []string{
			toolGetTides,
			toolGetWeather,
			toolGetSunriseSunset,
			toolGetSafetyAlerts,
			toolCalculateHeading,
		},
		includeSearch:            true,
		includeFacilityDiscovery: true,
		includeHydrographicPilot: true,
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
		// Specialist recommends anchorages and facilities. Facility/anchorage
		// discovery is delegated to the facility_discovery sub-agent; the
		// specialist keeps hydrographic pilotage plus web search for reviews
		// and cruising-guide context.
		name:                     Specialist,
		description:              "A Local Pilot and Navigation Specialist.",
		instruction:              prompt.Specialist,
		temperature:              0.25,
		toolNames:                nil,
		includeSearch:            true,
		includeFacilityDiscovery: true,
		includeHydrographicPilot: true,
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
			toolCalculateHeading,
			toolGetSafetyAlerts,
		},
		includeSearch:            false,
		includeHydrographicPilot: true,
	},
}

type agentConfig struct {
	name        string
	modelName   string
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

		modelName := cfg.ModelName
		if sp.name == Commodore && cfg.CommodoreModelName != "" {
			modelName = cfg.CommodoreModelName
		}
		slog.Info("config", "agent", sp.name, "model", modelName)

		a, err := createAgent(ctx, cfg, before, after, &agentConfig{
			name:        sp.name,
			modelName:   modelName,
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
	if sp.includeFacilityDiscovery {
		facilityTool, err := createFacilityDiscoveryTool(ctx, cfg, before, after, researcherTools)
		if err != nil {
			return nil, err
		}
		agentTools = append(agentTools, facilityTool)
	}
	if sp.includeHydrographicPilot {
		hydroTool, err := createHydrographicPilotTool(ctx, cfg, before, after, researcherTools)
		if err != nil {
			return nil, err
		}
		agentTools = append(agentTools, hydroTool)
	}
	return agentTools, nil
}

// toolByName returns the registered tool with the given name, or an error if
// no such tool exists in the provided set.
func toolByName(tools []adktool.Tool, name string) (adktool.Tool, error) {
	for _, t := range tools {
		if t.Name() == name {
			return t, nil
		}
	}
	return nil, fmt.Errorf("tool %q is not registered", name)
}

// createFacilityDiscoveryTool builds the facility_discovery sub-agent and wraps
// it as a single ADK tool that harbourmaster and specialist delegate to for
// marine facility and wild-anchorage discovery. The sub-agent combines
// FindPlacesNearby with the wrapped Google Search sub-agent (not raw
// geminitool.GoogleSearch), so it never mixes grounded search with function
// calling in a single model request.
func createFacilityDiscoveryTool(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, researcherTools []adktool.Tool) (adktool.Tool, error) {
	placesTool, err := toolByName(researcherTools, toolFindPlacesNearby)
	if err != nil {
		return nil, fmt.Errorf("facility_discovery: %w", err)
	}

	searchTools, err := createSearchTools(ctx, cfg, before, after, "facility_discovery_search_specialist")
	if err != nil {
		return nil, err
	}

	tools := append([]adktool.Tool{placesTool}, searchTools...)

	facilityAgent, err := createAgent(ctx, cfg, before, after, &agentConfig{
		name:        "facility_discovery",
		description: "Searches, validates, dedupes, and geocodes marine facilities and anchorages (marinas, moorings, yacht clubs, fuel docks, boat launches, and wild anchorages) within a geographic radius.",
		instruction: prompt.FacilityDiscovery,
		tools:       tools,
		temperature: 0.25,
	})
	if err != nil {
		return nil, err
	}

	return agenttool.New(facilityAgent, nil), nil
}

// createHydrographicPilotTool builds the hydrographic_pilot sub-agent and wraps
// it as a single ADK tool that harbourmaster, specialist, and lookout delegate
// to for official pilotage intelligence. It centralizes NOAA Coast Pilot and
// NGA Sailing Directions retrieval (territory routing, query construction, and
// consistent citation formatting) in one place instead of duplicating those
// instructions across every agent prompt. The sub-agent uses only the two RAG
// tools (no web search).
func createHydrographicPilotTool(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, researcherTools []adktool.Tool) (adktool.Tool, error) {
	sailingTool, err := toolByName(researcherTools, toolQuerySailingDirections)
	if err != nil {
		return nil, fmt.Errorf("hydrographic_pilot: %w", err)
	}
	coastPilotTool, err := toolByName(researcherTools, toolQueryCoastPilot)
	if err != nil {
		return nil, fmt.Errorf("hydrographic_pilot: %w", err)
	}

	hydroAgent, err := createAgent(ctx, cfg, before, after, &agentConfig{
		name:        "hydrographic_pilot",
		description: "Retrieves official hydrographic and pilotage intelligence (channel depths, bridge clearances, tidal rips, currents, anchorages, regulations, hazards) from NOAA Coast Pilot and NGA Sailing Directions, with consistent citations.",
		instruction: prompt.HydrographicPilot,
		tools:       []adktool.Tool{sailingTool, coastPilotTool},
		temperature: 0.1,
	})
	if err != nil {
		return nil, err
	}

	return agenttool.New(hydroAgent, nil), nil
}

func supportsThinkingBudget(model string) bool {
	return !strings.HasPrefix(model, "gemini-4")
}

func createAgent(ctx context.Context, cfg *config.Config, before llmagent.BeforeToolCallback, after llmagent.AfterToolCallback, acfg *agentConfig) (agent.Agent, error) {
	targetModel := acfg.modelName
	if targetModel == "" {
		targetModel = cfg.ModelName
	}

	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     genai.Ptr[float32](acfg.temperature),
	}

	if cfg.ThinkingBudget >= 0 && supportsThinkingBudget(targetModel) {
		budget := cfg.ThinkingBudget
		genConfig.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: &budget}
	}

	// Select the Gemini authentication backend:
	// 1. Vertex AI (genai.BackendVertexAI): Used when cfg.UseVertexAI is true (or
	//    GOOGLE_GENAI_USE_VERTEXAI="true"/"1" in the environment). Authenticates via
	//    Google Cloud Application Default Credentials (the service account IAM identity
	//    attached to Cloud Run / Vertex AI Agent Runtime, or local gcloud ADC).
	//    Does not use or require a GEMINI_API_KEY string.
	// 2. Google AI Studio (genai.BackendGeminiAPI, default): Authenticates using the
	//    static cfg.GeminiAPIKey string (GEMINI_API_KEY).
	var clientCfg *genai.ClientConfig
	useVertex := cfg.UseVertexAI || os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "true" || os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "1"
	if useVertex {
		loc := cfg.GeminiLocation
		if loc == "" {
			loc = os.Getenv("GOOGLE_CLOUD_LOCATION")
		}
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
	m, err := gemini.NewModel(ctx, targetModel, clientCfg)
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
