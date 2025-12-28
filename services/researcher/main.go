package main

import (
	"context"
	"log"
	"os"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()

	// 1. Initialize Gemini Model
	// We use gemini-2.0-flash-exp (or similar) for speed/cost efficiency in research tasks
	model, err := gemini.NewModel(ctx, "gemini-2.0-flash-exp", &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// 2. Define the Agent
	researchAgent, err := llmagent.New(llmagent.Config{
		Name:        "researcher_agent",
		Model:       model,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: `
			You are an expert sailing navigator and researcher acting as a Virtual Harbourmaster.
			
			Your Task:
			Given a location (Latitude/Longitude or Name), a Date, and a Search Radius, you must research the immediate area and return a detailed briefing.

			Research Requirements:
			1. Facilities: Identify anchorages, marinas, and mooring fields within the radius.
			   - For Anchorages: Find details on "holding ground" (mud, sand, rock), protection (which wind directions it shelters from), and depth.
			   - For Marinas: Find VHF channels and phone numbers.
			2. Environment: 
			   - Weather: Retrieve the specific forecast for the requested Date. If the date is >10 days away, provide historical averages for that month.
			   - Tides: Find the nearest tidal station and provide High/Low times and heights for that specific Date.

			Output Format:
			You must return ONLY valid JSON matching this structure. Do not include markdown formatting (like '''json).
			{
				"location_name": "Resolved Name of Location",
				"weather_summary": {
					"summary": "Short text summary of conditions",
					"wind_speed_kt": 15,
					"wind_direction": "NW",
					"wave_height_ft": 2.5
				},
				"tides": {
					"station_name": "Name of Tide Station",
					"events": [
						{"time": "06:30", "type": "High", "height_ft": 8.5},
						{"time": "12:45", "type": "Low", "height_ft": 1.2}
					]
				},
				"facilities": [
					{
						"name": "Name of Spot",
						"type": "Anchorage" | "Marina" | "Mooring",
						"latitude": 0.0,
						"longitude": 0.0,
						"details": {
							"protection": "N, NW, W",
							"holding": "Good holding in mud",
							"vhf": "66A",
							"phone": "555-0199"
						}
					}
				],
				"sources": ["list of urls used"]
			}

		Good Sources of information:
		https://www.navily.com

		`,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// 3. Launch the Server
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
