Based on the `boatagent` from **Navallog** and your design documents for **NavalPlan**, here are the step-by-step instructions to build the **Researcher Agent**.

This agent will act as your "Virtual Harbourmaster," taking a location and date to return structured data about anchorages, weather, and tides.

### Step 1: Create the Directory Structure

Create a new folder for the service inside your `navalplan` repository:

```bash
mkdir -p services/researcher
cd services/researcher
go mod init github.com/tpryan/navalplan/services/researcher

```

### Step 2: Create the Agent Code (`main.go`)

Create `services/researcher/main.go`. This code is adapted from your `boatagent`, but I have updated the **System Instruction** to match the "Virtual Harbourmaster" persona defined in your design doc.

I also added a structured JSON definition in the prompt to ensure the LLM returns data your backend can easily parse.

```go
package main

import (
	"context"
	"log"
	"os"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher/adk"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/server/restapi/services"
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
	agent, err := llmagent.New(llmagent.Config{
		Name:        "researcher_agent",
		Model:       model,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: `
			You are an expert sailing navigator and researcher acting as a Virtual Harbourmaster.
			
			Your Task:
			Given a location (Latitude/Longitude or Name), a Date, and a Search Radius, you must research the area and return a detailed briefing.

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
		`,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// 3. Launch the Server
	config := &adk.Config{
		AgentLoader: services.NewSingleAgentLoader(agent),
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

```

### Step 3: Install Dependencies

Run this in the `services/researcher` directory to fetch the ADK and GenAI libraries:

```bash
go get google.golang.org/adk
go get google.golang.org/genai
go mod tidy

```

### Step 4: Create Deployment Config (`cloudbuild-agent.yaml`)

Create `cloudbuild-agent.yaml` in the **root** of your `navalplan` repo (or inside `services/researcher` if you prefer, but root is standard).

This matches `navallog`'s config but targets the new `navalplan-researcher` service.

```yaml
steps:
- name: 'gcr.io/cloud-builders/gcloud'
  args:
  - 'run'
  - 'deploy'
  - 'navalplan-researcher'          # Service Name
  - '--source'
  - '.'
  - '--region'
  - 'us-central1'
  - '--project'
  - 'navalplan'                     # Your Google Cloud Project ID
  - '--no-allow-unauthenticated'    # Security: Only backend can call this
  dir: 'services/researcher'        # Build context
logsBucket: gs://navalplan-logging-bucket

```

### Step 5: How to Test Locally

You can run this agent locally and test it with `curl` before wiring it up to the frontend.

1. **Run the Agent:**
```bash
export GEMINI_API_KEY="your_api_key_here"
export PORT=8081
cd services/researcher
go run main.go

```


2. **Query it:**


The ADK exposes a standard REST endpoint. You query it by sending a prompt in the JSON body.


```bash


curl -X POST http://localhost:8081/run \


  -H "Content-Type: application/json" \


  -d '{


    "appName": "researcher_agent",


    "userId": "test_user",


    "sessionId": "test_session",


    "newMessage": {


      "role": "user",


      "parts": [{ "text": "Research anchorages and weather for 48.75 N, 123.22 W (Poets Cove) for July 15, 2025. Radius 5nm." }]


    }


  }'


```






### Summary of Differences from `boatagent`

1. **Instruction:** The prompt is significantly more complex, enforcing a specific schema for "Facilities" and "Weather" which matches your `openapi.yaml`.
2. **Tools:** It uses the same `GoogleSearch` tool, but the prompt guides it to look for nautical specific data (holding ground, VHF channels) rather than boat specs.