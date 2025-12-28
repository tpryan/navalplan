package main

import (
	"context"
	"log"
	"os"

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
			Your ONLY goal is to use the 'get_weather_forecast' tool to retrieve data for the requested location and date.
			Return the tool output directly.
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
			Synthesize the search results into a concise answer.
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
			1. WEATHER: 
			   - Check the requested Date.
			   - If the Date is within the next 10 days, call 'weather_specialist' with the exact date.
			   - If the Date is far in the future (>10 days), do NOT call the tool with that future date. Instead, calculate the date for the *same day and month* but in the *previous year* (e.g. if target is 2025-12-28, ask for 2024-12-28) and call 'weather_specialist' with that historical date.
			   - In your final summary, explicitly state: "Showing historical weather data from [Year] as an estimate."

			2. TIDES & FACILITIES: Call the 'search_specialist' tool to find:
			   - "Tide table for [Location] on [Date]"
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
					"station_name": "...",
					"events": [{"time": "...", "type": "...", "height_ft": 0}]
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
