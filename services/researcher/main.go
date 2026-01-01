package main

import (
	"context"
	"log"
	"os"
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
	// We use gemini-2.0-flash-001 as it is the current stable flash model
	model, err := gemini.NewModel(ctx, "gemini-2.0-flash-001", &genai.ClientConfig{
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
			- Do NOT provide conversational updates (e.g., "I am researching...").
			- Do NOT output the JSON structure until you have successfully called the tools and received data.
			- Do NOT output "Please wait" messages. The user is an API client, not a human chatting.

			EXECUTION PLAN (You MUST execute these tools first):
			1. WEATHER: Call the 'get_weather_forecast' tool to get precise forecast data for the specific location and date.
			2. TIDES: Call the 'get_tides' tool to get official NOAA tide predictions for the location and date.
			3. SUNRISE: Call the 'get_sunrise_sunset' tool to get sunrise and sunset times for the location and date.
			4. FACILITIES: Call the 'search_specialist' tool to find:
			   - "Anchorages near [Location] details coordinates"
			   - "Marina contact info [Location] coordinates"

			OUTPUT:
			Combine all findings into this JSON structure. Ensure "details" is always an object with descriptive keys, not a string.
			Try to find latitude and longitude for facilities if available.
			
			CRITICAL RULES:
			1. For 'tides.events': You MUST include ALL events returned by the 'get_tides' tool, including those from the previous and next days. The frontend needs the full 48-hour dataset for graphing. DO NOT filter the list.
			2. For 'tides.station_name': Use the EXACT station_name returned by the 'get_tides' tool. DO NOT substitute it with a more general or famous location.
			3. For 'weather_summary': Synthesize a readable sentence for the summary (e.g., "Expect clear skies with moderate westerly winds...").
			
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
					"station_name": "COPY_EXACT_STATION_NAME_FROM_TOOL_OUTPUT",
					"events": [
						{"time": "2025-05-01 06:30", "type": "High", "height_ft": 8.5},
						{"time": "2025-05-01 12:45", "type": "Low", "height_ft": 1.2}
					]
				},
				"facilities": [
					{
						"name": "...",
						"type": "Anchorage" | "Marina" | "Mooring",
						"latitude": 0.0,
						"longitude": 0.0,
						"details": {
							"description": "...",
							"protection": "...",
							"vhf": "..."
						},
						"references": ["https://...", "https://..."]
					}
				],
				"sources": [...]
			}

			Important: Always try to find a relevant URL for facilities. Always provide reference links. 

			Please also return a list of sources where you got the information. Please make sure they are valid and still active urls. Use them to populate urls for the content in the json. 
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
				{ "title": "Coral Heads", "description": "Numerous uncharted coral heads inside the reef.", "url": "http...", "references" : ["https://...", "https://..."]  },
				{ "title": "Christmas Winds", "description": "Strong trade winds (25-30kt) common in Dec/Jan.", "url": null, "references" : ["https://...", "https://..."] }
			  ],
			  "hubs": [
				{ "name": "Road Town", "description": "Major provisioning and charter hub.", "url": "http...", "references" : ["https://...", "https://..."] }
			  ],
			  "charter_info": {
				 "is_charter_destination": true,
				 "companies": [
					 { "name": "Moorings", "url": "https://...", "references" : ["https://...", "https://..."]},
					 { "name": "Dream Yacht", "url": "https://...", "references" : ["https://...", "https://..."]}
				 ]
			  },
			  "airports": [
			     { "name": "Terrance B. Lettsome International Airport", "iata_code": "EIS", "type": "International", "distance_km": 15, "references": ["https://..."] }
			  ],
			  "country_info": {
			     "name": "British Virgin Islands",
			     "languages": ["English"],
			     "timezone": "AST (UTC-4)",
			     "emergency_numbers": { "Police": "999", "Medical": "999" }
			  },
			  "currencies": [
			     { "name": "United States Dollar", "code": "USD", "symbol": "$" }
			  ],
			  "points_of_interest": [
			     { "name": "The Baths", "description": "Famous beach area on Virgin Gorda...", "references": ["https://..."] }
			  ]
			}

			Tools: Use Google Search to answer these specific questions:
			1. "Sailing season months for [Location]"
			2. "Hurricane season [Location]"
			3. "Sailing hazards and anomalies [Location]"
			4. "Major marinas and sailing hubs [Location]"
			5. "Yacht charter companies [Location]"
			6. "Nearest airports to [Location] for sailing"
			7. "Currency and language in [Location]"
			8. "Must-visit sailing points of interest [Location]"
			
			Important: Always try to find a relevant URL for hazards, hubs, charter companies, and points of interest. Always provide reference links. 

			Please also return a list of sources where you got the information. Please make sure they are valid and still active urls. Use them to populate urls for the content in the json.  
		`,
		Tools: []tool.Tool{
			agenttool.New(searchAgent, nil),
		},
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{onBeforeTool},
		AfterToolCallbacks:  []llmagent.AfterToolCallback{onAfterTool},
	})
	if err != nil {
		clog.Fatalf("Failed to create guide agent: %v", err)
	}

	// 5. Launch the Server
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
		port = "8081"
	}

	l := full.NewLauncher()
	err = l.Execute(ctx, config, []string{"web", "-read-timeout", "60s", "-write-timeout", "60s", "-port", port, "api"})
	if err != nil {
		clog.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
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
