package main

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	clog "github.com/charmbracelet/log"
	"github.com/tpryan/navalplan/services/researcher/tools"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/agenttool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

func main() {
	// Configure charmbracelet/log
	clog.SetOutput(os.Stdout)
	clog.SetLevel(clog.DebugLevel)
	clog.SetPrefix("agent")

	// Redirect standard log to charmbracelet/log
	stdLog := clog.StandardLog()
	log.SetOutput(stdLog.Writer())
	log.SetFlags(0)

	ctx := context.Background()

	// 1. Initialize Gemini Model
	// We use gemini-2.0-flash-001 as it is the current stable flash model, unless overridden by env var
	modelName := os.Getenv("NAVALPLAN_AGENT_MODEL")
	if modelName == "" {
		modelName = "gemini-2.0-flash-001"
	}

	key := ObscureString(os.Getenv("GEMINI_API_KEY"), os.Getenv("GEMINI_API_KEY"))

	clog.Info("config", "key", key)
	clog.Info("config", "modelName", modelName)

	model, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		clog.Fatalf("Failed to create model: %v", err)
	}

	weatherTool, err := tools.NewWeatherTool()
	if err != nil {
		clog.Fatalf("Failed to create weather tool: %v", err)
	}

	tideTool, err := tools.NewTideTool()
	if err != nil {
		clog.Fatalf("Failed to create tide tool: %v", err)
	}

	sunriseTool, err := tools.NewSunriseTool()
	if err != nil {
		clog.Fatalf("Failed to create sunrise tool: %v", err)
	}

	// 2. Define Sub-Agent (Search Specialist)
	searchAgent, err := llmagent.New(llmagent.Config{
		Name:        "search_specialist",
		Model:       model,
		Description: "Finds information on the web (facilities, reviews).",
		Instruction: `
			You are a Web Search Specialist.
			Your goal is to find specific information requested by the user using Google Search.
			Do NOT synthesize or summarize extensively. Return the relevant search snippets or data points directly and concisely.
		`,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
	})
	if err != nil {
		clog.Fatalf("Failed to create search agent: %v", err)
	}

	// 3. Define Parent Agent (Researcher / Orchestrator)
	// We wrap sub-agents as tools using agenttool.New
	researchAgent, err := llmagent.New(llmagent.Config{
		Name:        "researcher_agent",
		Model:       model,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: `
			You are an expert Virtual Harbourmaster.
			
			Your Goal: Produce a comprehensive JSON briefing for a sailing destination.

			RESTRICTIONS:
			- Do NOT provide conversational updates.
			- Do NOT output the JSON structure until you have successfully called the tools and received data.

			DATA GATHERING (Execute ALL of these in PARALLEL in the first turn):
			1. Call 'get_weather_forecast' for the location and date.
			2. Call 'get_tides' for the location and date.
			3. Call 'get_sunrise_sunset' for the location and date.
			4. Call 'search_specialist' multiple times (or once with a combined query) for:
			   - "Anchorages near [Location] details protection holding"
			   - "Marina contact info [Location] vhf phone"
			   - "Dinghy accessible bars and restaurants near [Location] waterfront"

			OUTPUT:
			Combine all findings into this JSON structure. 
			
			CRITICAL RULES:
			1. For 'tides.events': Include ALL events returned.
			2. For 'tides.station_name': Use the EXACT station_name from the tool.
			3. For 'weather_summary': Synthesize a readable sentence.
			4. For 'facilities': Include "Bar" and "Restaurant" types ONLY if they are accessible by water.
			
			{
				"location_name": "Resolved Name",
				"weather_summary": {
					"summary": "...",
					"condition": "...",
					"temp_min_f": 0,
					"temp_max_f": 0,
					"wind_speed_kt": 0,
					"wind_direction": "...",
					"wave_height_ft": 0,
					"debug_duration_ms": 0
				},
				"sun_phase": {
					"sunrise": "...",
					"sunset": "..."
				},
				"tides": {
					"station_name": "...",
					"events": [
						{"time": "2025-05-01 06:30", "type": "High", "height_ft": 8.5}
					]
				},
				"facilities": [
					{
						"name": "...",
						"type": "Anchorage" | "Marina" | "Mooring" | "Bar" | "Restaurant",
						"latitude": 0.0,
						"longitude": 0.0,
						"details": {
							"description": "...",
							"protection": "...",
							"vhf": "..."
						},
						"references": ["https://..."]
					}
				],
				"sources": [...]
			}

			Important: Always try to find a relevant URL for facilities. Always provide reference links. 
		`,
		Tools: []tool.Tool{
			weatherTool,
			tideTool,
			sunriseTool,
			agenttool.New(searchAgent, nil),
		},
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{onBeforeTool},
		AfterToolCallbacks:  []llmagent.AfterToolCallback{onAfterTool},
	})
	if err != nil {
		clog.Fatalf("Failed to create agent: %v", err)
	}

	// 4. Define Guide Agent
	guideAgent, err := llmagent.New(llmagent.Config{
		Name:        "guide_agent",
		Model:       model,
		Description: "A Local Knowledge Expert and Sailing Guide.",
		Instruction: `
			You are a Local Knowledge Expert and Sailing Guide.
			Task: Research the general sailing region for the location.
			
			DATA GATHERING (Execute multiple searches in PARALLEL):
			Call 'google_search' for:
			- "Sailing season months hurricane season [Location]"
			- "Sailing hazards coral reefs currents [Location]"
			- "Major sailing hubs marinas [Location]"
			- "Yacht charter companies [Location]"
			- "Nearest airports to [Location]"
			- "Currency language emergency numbers [Location]"
			- "Top sailing points of interest [Location]"

			Output: Produce a JSON object strictly following this schema:
			{
			  "summary": "A 2-3 sentence overview of sailing in this region.",
			  "sailing_season": {
				"primary_season_months": ["November", "December", ...],
				"storm_season_months": ["August", "September"],
				"storm_risk_level": "High/Medium/Low",
				"notes": "Hurricane season peaks in Sept.",
				"references" : ["https://...", "https://..."]
			  },
			  "hazards": [
				{ "title": "...", "description": "...", "url": "...", "references" : [...] }
			  ],
			  "hubs": [
				{ "name": "...", "description": "...", "url": "...", "references" : [...] }
			  ],
			  "charter_info": {
				 "is_charter_destination": true,
				 "companies": [
					 { "name": "...", "url": "...", "references" : [...]}
				 ]
			  },
			  "airports": [
			     { "name": "...", "iata_code": "...", "type": "...", "distance_km": 0, "references": [...] }
			  ],
			  "country_info": {
			     "name": "...",
			     "languages": ["..."],
			     "timezone": "...",
			     "emergency_numbers": { "Police": "..." }
			  },
			  "currencies": [
			     { "name": "...", "code": "...", "symbol": "..." }
			  ],
			  "points_of_interest": [
			     { "name": "...", "description": "...", "references": [...] }
			  ]
			}
			
			Important: Always provide reference links for every section.
		`,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{onBeforeTool},
		AfterToolCallbacks:  []llmagent.AfterToolCallback{onAfterTool},
	})
	if err != nil {
		clog.Fatalf("Failed to create guide agent: %v", err)
	}

	// 4. Launch the Server
	loader, err := agent.NewMultiLoader(researchAgent, guideAgent)
	if err != nil {
		clog.Fatalf("Failed to create multi loader: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: loader,
	}

	// Recovery for main process
	defer func() {
		if r := recover(); r != nil {
			clog.Printf("Recovered from panic in main: %v", r)
		}
	}()

	// Port handling for Cloud Run compatibility
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("NAVALPLAN_AGENT_PORT")
	}
	if port == "" {
		port = "8081" // Default fallback
	}

	l := full.NewLauncher()
	err = l.Execute(ctx, config, []string{"web", "-read-timeout", "60s", "-write-timeout", "60s", "-port", port, "api"})
	if err != nil {
		clog.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}

func ObscureString(input, toObscure string) string {
	// The number of runes (characters) in the input determines the length of the output.
	str := strings.Repeat("*", len(toObscure))
	return strings.ReplaceAll(input, toObscure, str)

}

var toolTimings sync.Map

func onBeforeTool(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
	toolTimings.Store(ctx.FunctionCallID(), time.Now())
	return nil, nil
}

func onAfterTool(ctx tool.Context, t tool.Tool, args map[string]any, result map[string]any, err error) (map[string]any, error) {
	if startTime, ok := toolTimings.LoadAndDelete(ctx.FunctionCallID()); ok {
		duration := time.Since(startTime.(time.Time))
		clog.Info("Tool performance", "tool", t.Name(), "duration", duration)
	}
	return result, nil
}
