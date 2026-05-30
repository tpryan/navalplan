### Architecture: The "Divide & Conquer" Model

We will move from a single "do-it-all" prompt to a **Tool-Augmented Agent**. This ensures that deterministic data (numbers) comes from code, while qualitative data (reviews/descriptions) comes from the LLM.

1. **Weather:** Handled by `tools.WeatherTool` (wrapping your `openmeteogo` library).
2. **Tides:** Handled by `tools.TideTool` (wrapping NOAA/WorldTides APIs).
3. **Facilities:** Handled by the LLM via `GoogleSearch` (finding anchorages, marinas, and reviews).

---

### Step 1: Manage Dependencies

First, ensure your service can access your custom library.

```bash
# Inside code/services/researcher
go get github.com/tpryan/openmeteogo
go mod tidy

```

---

### Step 2: Implement the Weather Tool

Create `code/services/researcher/tools/weather.go`. This wrapper connects the generic ADK interface to your strongly-typed `openmeteogo` library.

```go
package tools

import (
	"fmt"
	"sync"
	"time"

	"github.com/tpryan/openmeteogo"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// WeatherArgs defines the arguments for the get_weather_forecast tool.
type WeatherArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

// WeatherResult defines the response structure for the get_weather_forecast tool.
type WeatherResult struct {
	Date            string  `json:"date"`
	Condition       string  `json:"condition"`
	ForecastType    string  `json:"forecast_type"`
	MaxTemp         float64 `json:"max_temp"`
	MinTemp         float64 `json:"min_temp"`
	MaxWindKts      float64 `json:"max_wind_kts"`
	MaxGustsKts     float64 `json:"max_gusts_kts"`
	WindDirDeg      int     `json:"wind_dir_deg"`
	WindDirection   string  `json:"wind_direction"`
	PrecipTotal     float64 `json:"precip_total"`
	WaveHeight      float64 `json:"wave_height"`
	WaveDirection   float64 `json:"wave_direction"`
	WavePeriod      float64 `json:"wave_period"`
	DebugDurationMS int64   `json:"debug_duration_ms"`
}

// WeatherProvider implements the get_weather_forecast tool using the Open-Meteo API.
type WeatherProvider struct {
	client *openmeteogo.Client
}

// NewWeatherTool creates a new ADK tool for retrieving weather forecasts.
func NewWeatherTool() (tool.Tool, error) {
	wp := &WeatherProvider{
		client: openmeteogo.NewClient(),
	}
	return functiontool.New(functiontool.Config{
		Name:        "get_weather_forecast",
		Description: "Retrieves precise weather forecasts (Wind, Gusts, Temp, Waves) for a specific location and date.",
	}, wp.GetWeatherForecast)
}

func (wp *WeatherProvider) GetWeatherForecast(ctx tool.Context, args WeatherArgs) (WeatherResult, error) {
    // ... implementation ...
}
```

---

### Step 3: Implement the Tides Tool

Create `code/services/researcher/tools/tides.go`.

```go
package tools

import (
	"fmt"
	"time"

	"github.com/tpryan/noaago"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// TideArgs defines the arguments for the get_tides tool.
type TideArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

// TideResult defines the response structure for the get_tides tool.
type TideResult struct {
	StationName   string      `json:"station_name"`
	StationID     string      `json:"station_id"`
	DistanceMiles float64     `json:"distance_miles"`
	Tides         []TideEvent `json:"tides"`
}

// TideProvider implements the get_tides tool using the NOAA CO-OPS API.
type TideProvider struct {
	client *noaago.Client
}

// NewTideTool creates a new ADK tool for retrieving tide predictions.
func NewTideTool() (tool.Tool, error) {
	client := noaago.NewClient()
	tp := &TideProvider{client: client}

	return functiontool.New(functiontool.Config{
		Name:        "get_tides",
		Description: "Retrieves high and low tide predictions for a specific date from the nearest NOAA station.",
	}, tp.GetTides)
}

func (tp *TideProvider) GetTides(ctx tool.Context, args TideArgs) (TideResult, error) {
    // ... implementation ...
}
```

---

### Step 4: The Agent Service (`main.go`)

Update `code/services/researcher/main.go` to register these tools and use a refined system instruction.

```go
func (s *Server) createResearcherAgent() (agent.Agent, error) {
	genConfig := &genai.GenerateContentConfig{
		MaxOutputTokens: 65536,
		Temperature:     genai.Ptr[float32](0.4),
	}

	weatherTool, err := tools.NewWeatherTool()
	if err != nil {
		return nil, err
	}

	tideTool, err := tools.NewTideTool()
	if err != nil {
		return nil, err
	}

	sunriseTool, err := tools.NewSunriseTool()
	if err != nil {
		return nil, err
	}

	placesTool, err := tools.NewPlacesTool()
	if err != nil {
		return nil, err
	}

	// Create a dedicated model instance
	m, err := s.createModel()
	if err != nil {
		return nil, err
	}

	// 2. Define Sub-Agent (Search Specialist)
	searchAgent, err := llmagent.New(llmagent.Config{
		Name:        "search_specialist",
		Model:       m,
		Description: "Finds information on the web (facilities, reviews).",
		Instruction: searchSpecialistPrompt,
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
		GenerateContentConfig: genConfig,
	})
	if err != nil {
		return nil, err
	}

	// 3. Define Parent Agent (Researcher / Orchestrator)
	return llmagent.New(llmagent.Config{
		Name:        "researcher_agent",
		Model:       m,
		Description: "A Virtual Harbourmaster that researches sailing destinations.",
		Instruction: researcherAgentPrompt,
		Tools: []tool.Tool{
			weatherTool,
			tideTool,
			sunriseTool,
			placesTool,
			agenttool.New(searchAgent, nil),
		},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{s.onBeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{s.onAfterTool},
		GenerateContentConfig: genConfig,
	})
}
```