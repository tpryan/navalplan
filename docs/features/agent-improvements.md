### Architecture: The "Divide & Conquer" Model

We will move from a single "do-it-all" prompt to a **Tool-Augmented Agent**. This ensures that deterministic data (numbers) comes from code, while qualitative data (reviews/descriptions) comes from the LLM.

1. **Weather:** Handled by `tools.WeatherTool` (wrapping your `openmeteogo` library).
2. **Tides:** Handled by `tools.TideTool` (wrapping NOAA/WorldTides APIs).
3. **Facilities:** Handled by the LLM via `GoogleSearch` (finding anchorages, marinas, and reviews).

---

### Step 1: Manage Dependencies

First, ensure your service can access your custom library.

```bash
# Inside services/researcher
go get github.com/tpryan/openmeteogo
go mod tidy

```

---

### Step 2: Implement the Weather Tool

Create `services/researcher/tools/weather.go`. This wrapper connects the generic ADK interface to your strongly-typed `openmeteogo` client.

```go
package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/tpryan/openmeteogo"
	"google.golang.org/adk/tool"
)

type WeatherTool struct{}

func (t WeatherTool) Description() string {
	return "Retrieves precise weather forecasts (Wind, Gusts, Temp) for a specific location and date."
}

func (t WeatherTool) Schema() *tool.Schema {
	return &tool.Schema{
		Name: "get_weather_forecast",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"latitude":  map[string]any{"type": "number", "description": "Decimal latitude"},
				"longitude": map[string]any{"type": "number", "description": "Decimal longitude"},
				"date":      map[string]any{"type": "string", "description": "Date in YYYY-MM-DD format"},
			},
			"required": []string{"latitude", "longitude", "date"},
		},
	}
}

func (t WeatherTool) Call(ctx context.Context, input map[string]any) (any, error) {
	// 1. Parse Inputs safely
	lat, okLat := input["latitude"].(float64)
	lon, okLon := input["longitude"].(float64)
	dateStr, okDate := input["date"].(string)

	if !okLat || !okLon || !okDate {
		return nil, fmt.Errorf("invalid arguments: lat, lon, and date are required")
	}

	targetDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid date format: %v", err)
	}

	// 2. Initialize Client
	c := openmeteogo.NewClient()

	// 3. Build Options
	// We request specific daily metrics critical for sailing.
	opts := openmeteogo.NewOptionsBuilder().
		Latitude(lat).
		Longitude(lon).
		TemperatureUnit(openmeteogo.Fahrenheit).
		WindspeedUnit(openmeteogo.Knots). // Ensure your library supports this or handle conversion
		Start(targetDate).
		End(targetDate).
		DailyMetrics(openmeteogo.Metrics{
			openmeteogo.WeatherCode,
			openmeteogo.Temperature2mMax,
			openmeteogo.Temperature2mMin,
			openmeteogo.WindSpeed10mMax,
			openmeteogo.WindGusts10mMax,
			openmeteogo.WindDirection10mDominant,
			openmeteogo.PrecipitationSum,
		}).
		Build()

	// 4. Fetch Data
	weather, err := c.Get(opts)
	if err != nil {
		return nil, fmt.Errorf("openmeteogo error: %w", err)
	}

	if len(weather.Daily.Time) == 0 {
		return "No weather data available for this date.", nil
	}

	// 5. Format Output
	// Use your library's helper to describe the weather code
	desc := openmeteogo.DescribeCode(weather.Daily.WeatherCode[0])

	return map[string]any{
		"date":           weather.Daily.Time[0],
		"summary":        desc,
		"max_temp":       weather.Daily.Temperature2mMax[0],
		"min_temp":       weather.Daily.Temperature2mMin[0],
		"max_wind_kts":   weather.Daily.WindSpeed10mMax[0],
		"max_gusts_kts":  weather.Daily.WindGusts10mMax[0],
		"wind_dir_deg":   weather.Daily.WindDirection10mDominant[0],
		"precip_total":   weather.Daily.PrecipitationSum[0],
	}, nil
}

```

---

### Step 3: Implement the Tides Tool

Create `services/researcher/tools/tides.go`. Since we don't have a custom library for this yet, we will wrap a standard API (like NOAA CO-OPS for US waters) or keep it simple for now.

```go
package tools

import (
	"context"
	"fmt"
	"net/http"
	"encoding/json"
	"time"

	"google.golang.org/adk/tool"
)

type TideTool struct{}

func (t TideTool) Description() string {
	return "Retrieves high and low tide predictions for a specific date."
}

func (t TideTool) Schema() *tool.Schema {
	return &tool.Schema{
		Name: "get_tides",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"latitude":  map[string]any{"type": "number"},
				"longitude": map[string]any{"type": "number"},
				"date":      map[string]any{"type": "string"},
			},
			"required": []string{"latitude", "longitude", "date"},
		},
	}
}

func (t TideTool) Call(ctx context.Context, input map[string]any) (any, error) {
	// Implementation Note:
	// For V1, if you lack a global tide API key, you can return a specific string
	// telling the LLM to use Google Search as a fallback.
	// Or implement the NOAA API here if your users are primarily in the US.
	
	// Example fallback:
	return "Tide API unavailable. Please search for 'Tide table [Location] [Date]'", nil
}

```

---

### Step 4: The Agent Service (`main.go`)

Update `services/researcher/main.go` to register these tools and use a refined system instruction.

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/tpryan/navalplan/services/researcher/tools" // Import your new tools package
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

	// 1. Model Init
	model, err := gemini.NewModel(ctx, "gemini-2.0-flash-exp", &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// 2. Agent Definition
	agent, err := llmagent.New(llmagent.Config{
		Name:        "researcher_agent",
		Model:       model,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: `
			You are an expert Virtual Harbourmaster.
			
			Your Goal: Produce a comprehensive JSON briefing for a sailing destination.

			EXECUTION PLAN:
			1. WEATHER: ALWAYS use the 'get_weather_forecast' tool first.
			   - Trust the tool's output for Wind Speed, Gusts, and Direction.
			   - Do not guess or hallucinate weather numbers.
			
			2. TIDES: Use 'get_tides'. If it returns "unavailable", use 'google_search' to find "Tide table for [Location] on [Date]".
			   - Extract High/Low times and heights.

			3. FACILITIES: Use 'google_search' to find qualitative details.
			   - Search for: "Anchorages near [Location] holding ground protection"
			   - Search for: "[Marina Name] VHF channel phone number"
			   - Synthesize reviews into a short summary.

			OUTPUT:
			Return strictly valid JSON matching the schema provided by the user system.
			{
				"weather_summary": { ... },
				"tides": { ... },
				"facilities": [ ... ]
			}
		`,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{}, // Native Search
			tools.WeatherTool{},       // Your Custom OpenMeteo Tool
			tools.TideTool{},          // Your Custom Tide Tool
		},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// 3. Launch
	config := &adk.Config{
		AgentLoader: services.NewSingleAgentLoader(agent),
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	l := full.NewLauncher()
	if err := l.Execute(ctx, config, []string{"web", "-port", port, "api"}); err != nil {
		log.Fatalf("run failed: %v", err)
	}
}

```