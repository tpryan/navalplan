Here is the technical implementation plan to parallelize the `researcher` agent.

### **Technical Implementation Plan: Parallel Research Agent**

**Objective:**
Reduce the latency of the Researcher Agent by executing independent data retrieval tasks (Weather, Tides, Sun Phase, Places) concurrently.

**Architecture:**
Transition from a **Serial Agent** (single loop) to a **Parallel Agent** architecture.

* **Orchestrator:** `ParallelAgent` (Persona: Virtual Harbourmaster)
* **Sub-Agents (Environmental):** `WeatherAgent`, `TidesAgent`, `SunAgent`
* **Sub-Agents (Facilities):** `MarinaAgent`, `ProvisionsAgent`, `ServicesAgent`

---

### **Implementation Steps**

#### **1. Refactor `services/researcher/main.go**`

Replace the existing agent initialization in `main.go` with the following implementation. This setup initializes all tools and wraps them in specialized agents, which are then orchestrated by the `ParallelAgent`.

```go
package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/google/adk/agents"
	"github.com/google/adk/models/gemini"
	"github.com/joho/godotenv" // Assuming this is used for .env loading
	"navalplan/services/researcher/tools"
)

//go:embed prompts/researcher_agent.md
var researcherPrompt string

func main() {
	// 1. Environment & Model Setup
	_ = godotenv.Load()
	ctx := context.Background()
	
	model, err := gemini.NewClient(ctx, func(o *gemini.ClientOptions) {
		o.Model = "gemini-1.5-pro-002" // Use a performant model
	})
	if err != nil {
		log.Fatal(err)
	}

	// 2. Initialize Tools
	// These tools will be distributed among the specialized agents.
	weatherTool := tools.NewWeatherTool()
	tidesTool := tools.NewTidesTool()
	sunTool := tools.NewSunriseTool()
	placesTool := tools.NewPlacesTool()

	// 3. Define Specialized Sub-Agents
	// Each agent focuses on one domain to prevent context pollution and ensure accurate tool usage.

	// --- Environmental Specialists ---
	weatherAgent := agents.NewLlmAgent(
		agents.WithLlmAgentModel(model),
		agents.WithLlmAgentInstruction("You are a weather specialist. Retrieve the 7-day forecast for the requested location. Return the raw data."),
		agents.WithTools(weatherTool),
	)

	tidesAgent := agents.NewLlmAgent(
		agents.WithLlmAgentModel(model),
		agents.WithLlmAgentInstruction("You are a tides specialist. Retrieve tidal predictions for the requested location. Return the raw data."),
		agents.WithTools(tidesTool),
	)

	sunAgent := agents.NewLlmAgent(
		agents.WithLlmAgentModel(model),
		agents.WithLlmAgentInstruction("You are a sun phase specialist. Retrieve sunrise and sunset times. Return the raw data."),
		agents.WithTools(sunTool),
	)

	// --- Facility Specialists (Parallel "Scouts") ---
	marinaAgent := agents.NewLlmAgent(
		agents.WithLlmAgentModel(model),
		agents.WithLlmAgentInstruction("You are a marina specialist. Find marinas, anchorages, and dinghy docks. Focus on depth, slip availability, and VHF channels."),
		agents.WithTools(placesTool),
	)

	provisionsAgent := agents.NewLlmAgent(
		agents.WithLlmAgentModel(model),
		agents.WithLlmAgentInstruction("You are a provisions specialist. Find grocery stores, fresh markets, and restaurants within walking distance of the waterfront."),
		agents.WithTools(placesTool),
	)

	servicesAgent := agents.NewLlmAgent(
		agents.WithLlmAgentModel(model),
		agents.WithLlmAgentInstruction("You are a marine services specialist. Find fuel docks, marine mechanics, chandleries, and pump-out stations."),
		agents.WithTools(placesTool),
	)

	// 4. Create the Orchestrator (Parallel Agent)
	// This agent sends the user prompt to all sub-agents simultaneously, waits for their responses,
	// and then uses the 'researcherPrompt' (Harbourmaster persona) to aggregate the final report.
	researcherAgent := agents.NewParallelAgent(
		"naval_researcher",
		[]agents.Agent{
			weatherAgent,
			tidesAgent,
			sunAgent,
			marinaAgent,
			provisionsAgent,
			servicesAgent,
		},
		agents.WithParallelAgentModel(model),
		agents.WithParallelAgentInstruction(researcherPrompt),
	)

	// 5. Start the Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Researcher Agent listening on port %s", port)
	http.Handle("/agent", agents.NewHandler(researcherAgent))
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

```

#### **2. Verify Prompt Configuration (`prompts/researcher_agent.md`)**

Ensure your existing `researcher_agent.md` is set up to receive raw data and format it. It acts as the "Aggregator" in this pattern.

* **Role:** Virtual Harbourmaster.
* **Task:** Receive the outputs from the weather, tides, and facility agents and synthesize them into a single, authoritative Briefing Report.
* **Style:** Professional, nautical, concise.

#### **3. Performance Impact**

* **Previous State (Sequential):**
`Total Time = Time(Weather) + Time(Tides) + Time(Sun) + Time(Places*3)`
* **New State (Parallel):**
`Total Time = Max(Time(Weather), Time(Tides), Time(Sun), Time(Places)) + AggregationTime`

This change effectively flattens the execution timeline, making the agent significantly more responsive.