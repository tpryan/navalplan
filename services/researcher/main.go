package main

import (
	"context"
	"os"

	"github.com/charmbracelet/log"
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
	ctx := context.Background()

	// 1. Initialize Gemini Model
	// We use gemini-2.0-flash-001 as it is the current stable flash model
	model, err := gemini.NewModel(ctx, "gemini-2.0-flash-001", &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	weatherTool, err := tools.NewWeatherTool()
	if err != nil {
		log.Fatalf("Failed to create weather tool: %v", err)
	}

	// 2. Define Sub-Agent (Weather Specialist)
	weatherAgent, err := llmagent.New(llmagent.Config{
		Name:        "weather_specialist",
		Model:       model,
		Description: "Retrieves precise weather forecasts.",
		Instruction: `
			You are a Weather Specialist.
			1. Use the 'get_weather_forecast' tool for the requested location and date.
			2. You MUST reply to the user with the JSON output from the tool. Do not add conversational text, just the data.
		`,
		Tools: []tool.Tool{weatherTool},
	})
	if err != nil {
		log.Fatalf("Failed to create weather agent: %v", err)
	}

	// 3. Define Sub-Agent (Search Specialist)
	searchAgent, err := llmagent.New(llmagent.Config{
		Name:        "search_specialist",
		Model:       model,
		Description: "Finds information on the web (tides, facilities, reviews).",
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
		log.Fatalf("Failed to create search agent: %v", err)
	}

	// 4. Define Parent Agent (Researcher / Orchestrator)
	// We wrap sub-agents as tools using agenttool.New
	researchAgent, err := llmagent.New(llmagent.Config{
		Name:        "researcher_agent",
		Model:       model,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: `
			You are an expert Virtual Harbourmaster.
			
			Your Goal: Produce a comprehensive JSON briefing for a sailing destination.

			EXECUTION PLAN:
			1. WEATHER: Call the 'weather_specialist' tool to get precise forecast data.
			2. TIDES & FACILITIES: Call the 'search_specialist' tool to find:
			   - "Tide table for [Location] for [Date], [Date - 1 day], and [Date + 1 day]" (We need surrounding days for context).
			   - "Anchorages near [Location] details"
			   - "Marina contact info [Location]"

			OUTPUT:
			Combine all findings into this JSON structure. Ensure "details" is always an object with descriptive keys, not a string.
			{
				"location_name": "Resolved Name",
				"weather_summary": {
					"summary": "...",
					"wind_speed_kt": 0,
					"wind_direction": "...",
					"wave_height_ft": 0
				},
				"tides": {
					"station_name": "Name of Tide Station",
					"events": [
						{"time": "2025-05-01 06:30", "type": "High", "height_ft": 8.5},
						{"time": "2025-05-01 12:45", "type": "Low", "height_ft": 1.2}
					]
				},
				"facilities": [
					{
						"name": "...",
						"type": "Anchorage" | "Marina" | "Mooring",
						"details": {
							"description": "...",
							"protection": "...",
							"vhf": "..."
						}
					}
				],
				"sources": [...]
			}
		`,
		Tools: []tool.Tool{
			agenttool.New(weatherAgent, nil),
			agenttool.New(searchAgent, nil),
		},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// 5. Launch the Server
	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(researchAgent),
	}

	// Recovery for main process
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic in main: %v", r)
		}
	}()

	// Port handling for Cloud Run compatibility
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	l := full.NewLauncher()
	err = l.Execute(ctx, config, []string{"web", "-port", port, "api"})
	if err != nil {
		log.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
