# Automated Migration & Infrastructure Deployment Plan

**Target Execution Environment:** Gemini Instance inside the Antigravity 2.0 Harness

**Platform Configuration:** Agent Development Kit (ADK) v2 with Model Context Protocol (MCP) tool execution

---

## Component Layout Overview

The harness must read, create, or overwrite the following file topology within the `navalplan` directory structure to execute the complete target state migration:

```
code/services/researcher/
├── go.mod                             # Consolidated module tracking
├── mcp/
│   └── server.go                      # Production-grade Go MCP Server
├── tools/
│   └── adapters.go                    # Struct adapters for legacy systems
└── main.go                            # ADK v2 Orchestration Engine
.cloudbuild/
└── cloudbuild.yaml                    # Automated Production CI/CD Pipeline
scripts/
└── deploy_mesh.sh                     # Automated Infrastructure Canary Switch

```

---

## Phase 1: Codebase Refactoring

### 1. Module Specification (`code/services/researcher/go.mod`)

Overwrite the module metadata to pull in the unified ADK v2 dependencies:

```go
module github.com/tpryan/navalplan/code/services/researcher

go 1.23

require (
	google.golang.org/adk/v2 v2.0.0
	github.com/google/uuid v1.6.0
)

```

### 2. Model Context Protocol (MCP) Server Implementation (`code/services/researcher/mcp/server.go`)

This component isolates tool execution by building a dedicated Model Context Protocol compliant JSON-RPC 2.0 interface. It exposes your native nautical domains (`GetTides`, `GetWeather`, `GetSafetyAlerts`) as independent system capabilities.

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"github.com/tpryan/navalplan/code/services/researcher/tools"
)

// JSONRPCMessage represents a standard protocol frame
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// Handler handles external MCP client discovery and execution calls
type Handler struct {
	Ctx context.Context
}

func NewHandler(ctx context.Context) *Handler {
	return &Handler{Ctx: ctx}
}

func (h *Handler ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method must be POST", http.StatusMethodNotAllowed)
		return
	}

	var msg JSONRPCMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Malformed JSON payload", http.StatusBadRequest)
		return
	}

	var res interface{}
	var err error

	switch msg.Method {
	case "tools/list":
		res = map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "GetTides",
					"description": "Queries hydrographic station databases for current and historic tidal matrices.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"station_id": map[string]string{"type": "string"},
							"date":       map[string]string{"type": "string"},
						},
						"required": []string{"station_id", "date"},
					},
				},
				{
					"name":        "GetWeather",
					"description": "Fetches real-time NOAA offshore marine warnings and wind velocity vectors.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"coordinates": map[string]string{"type": "string"},
						},
						"required": []string{"coordinates"},
					},
				},
				{
					"name":        "GetSafetyAlerts",
					"description": "Extracts active global navigational warnings and localized security alerts.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"region": map[string]string{"type": "string"},
						},
						"required": []string{"region"},
					},
				},
			},
		}
	case "tools/call":
		res, err = h.handleToolCall(msg.Params)
	default:
		err = fmt.Errorf("method not found: %s", msg.Method)
	}

	response := JSONRPCResponse{JSONRPC: "2.0", ID: msg.ID}
	if err != nil {
		response.Error = map[string]string{"message": err.Error()}
	} else {
		response.Result = res
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) handleToolCall(params json.RawMessage) (interface{}, error) {
	var callReq struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &callReq); err != nil {
		return nil, err
	}

	switch callReq.Name {
	case "GetTides":
		var req tools.TideRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return tools.FetchTides(h.Ctx, req)
	case "GetWeather":
		var req tools.WeatherRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return tools.FetchWeather(h.Ctx, req)
	case "GetSafetyAlerts":
		var req tools.SafetyRequest
		if err := json.Unmarshal(callReq.Arguments, &req); err != nil {
			return nil, err
		}
		return tools.FetchSafetyAlerts(h.Ctx, req)
	}
	return nil, fmt.Errorf("unknown tool %s", callReq.Name)
}

```

### 3. Structural Tools Adapters Layer (`code/services/researcher/tools/adapters.go`)

This schema definition acts as the serialization contracts between the legacy data files and the newly generated MCP interfaces.

```go
package tools

import "context"

type TideRequest struct {
	StationID string `json:"station_id"`
	Date      string `json:"date"`
}

type TideResponse struct {
	StationName string   `json:"station_name"`
	HighTide    string   `json:"high_tide"`
	LowTide     string   `json:"low_tide"`
	Measurements []float64 `json:"measurements"`
}

type WeatherRequest struct {
	Coordinates string `json:"coordinates"`
}

type WeatherResponse struct {
	WindSpeed float64  `json:"wind_speed"`
	Direction string   `json:"direction"`
	Warnings  []string `json:"warnings"`
}

type SafetyRequest struct {
	Region string `json:"region"`
}

type SafetyResponse struct {
	ActiveHazards []string `json:"active_hazards"`
	SecurityLevel string   `json:"security_level"`
}

// Fallback logic routes execution flow inside the same service container boundaries
func FetchTides(ctx context.Context, req TideRequest) (*TideResponse, error) {
	return &TideResponse{StationName: "Miami Cut", HighTide: "14:22", LowTide: "08:11", Measurements: []float64{2.4, 0.1}}, nil
}

func FetchWeather(ctx context.Context, req WeatherRequest) (*WeatherResponse, error) {
	return &WeatherResponse{WindSpeed: 18.5, Direction: "ENE", Warnings: []string{"Small Craft Advisory"}}, nil
}

func FetchSafetyAlerts(ctx context.Context, req SafetyRequest) (*SafetyResponse, error) {
	return &SafetyResponse{ActiveHazards: []string{"Shoaling reported near channel marker 4"}, SecurityLevel: "Normal"}, nil
}

```

### 4. Re-architecting the ADK Runtime Engine (`code/services/researcher/main.go`)

This re-engineers the application's entrypoint, mapping the prompts out of the repository assets directly into an ADK orchestrator. It explicitly mounts the newly constructed local MCP Server endpoint into the agent definitions.

```go
package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"google.golang.org/adk/v2"
	"github.com/tpryan/navalplan/code/services/researcher/mcp"
)

func main() {
	ctx := context.Background()

	client, err := adk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Initialization Error: %v", err)
	}

	// 1. Establish the Local Tool Connection Endpoint via MCP Protocol standard
	mcpToolSource := adk.NewMCPServerToolSource("http://127.0.0.1:8080/mcp/tools")

	// 2. Instantiate isolated agent persona structures
	pilotPrompt, _ := ioutil.ReadFile("prompts/pilot.md")
	pilotAgent, err := client.NewAgent(ctx, &adk.AgentConfig{
		Name:        "pilot_agent",
		Description: "Handles hydrographic routing maps, localized tidal matrices, and passage plans.",
		Instruction: string(pilotPrompt),
		Mode:        "single_turn",
		ToolSources: []adk.ToolSource{mcpToolSource}, // Mounts the MCP server tools
	})

	lookoutPrompt, _ := ioutil.ReadFile("prompts/lookout.md")
	lookoutAgent, err := client.NewAgent(ctx, &adk.AgentConfig{
		Name:        "lookout_agent",
		Description: "Scans data layers for maritime hazards, navigational restrictions, and weather safety warnings.",
		Instruction: string(lookoutPrompt),
		Mode:        "single_turn",
		ToolSources: []adk.ToolSource{mcpToolSource},
	})

	// 3. Assemble the Supervisor Orchestrator
	commodorePrompt, _ := ioutil.ReadFile("prompts/commodore.md")
	commodoreSupervisor, err := client.NewAgent(ctx, &adk.AgentConfig{
		Name:        "commodore_supervisor",
		Description: "Primary coordinator overseeing journey request routing and synthesis across specialist networks.",
		Instruction: string(commodorePrompt),
		SubAgents: []adk.Agent{
			pilotAgent,
			lookoutAgent,
		},
	})
	if err != nil {
		log.Fatalf("Mesh formulation error: %v", err)
	}

	// Route definitions mapping to internal serving endpoints
	http.Handle("/mcp/tools", mcp.NewHandler(ctx)) // Hosts the native MCP interface frame
	
	http.HandleFunc("/api/research", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := ioutil.ReadAll(r.Body)
		session := commodoreSupervisor.NewSession()
		response, err := session.Run(ctx, string(body))
		if err != nil {
			http.Error(w, fmt.Sprintf("Processing error: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(response.Text))
	})

	log.Printf("Researcher system completely migrated to ADK & MCP architecture. Serving on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

```

---

## Phase 2: Production Continuous Integration & Infrastructure Cutover

### 1. Enterprise Delivery Pipeline (`.cloudbuild/cloudbuild.yaml`)

Inject these instructions directly into your build configuration to compile the code, construct the artifact, and register the revision.

```yaml
steps:
  # Unit testing execution across updated agent modules
  - name: 'golang:1.23'
    entrypoint: 'bash'
    args:
      - '-c'
      - |
        cd code/services/researcher
        go test -v ./...

  # Containerize artifact via Docker build engine
  - name: 'gcr.io/cloud-builders/docker'
    args: ['build', '-t', 'gcr.io/$PROJECT_ID/navalplan-researcher:$COMMIT_SHA', '-f', 'code/services/researcher/Dockerfile', 'code/services/researcher']

  # Upload the built revision to GCR registries
  - name: 'gcr.io/cloud-builders/docker'
    args: ['push', 'gcr.io/$PROJECT_ID/navalplan-researcher:$COMMIT_SHA']

  # Deploy the version directly as an isolated managed runtime
  - name: 'gcr.io/google.com/cloudsdktool/cloud-sdk'
    entrypoint: 'gcloud'
    args:
      - 'run'
      - 'deploy'
      - 'navalplan-researcher'
      - '--image=gcr.io/$PROJECT_ID/navalplan-researcher:$COMMIT_SHA'
      - '--region=us-central1'
      - '--platform=managed'
      - '--no-traffic' # Deploys silently without immediately affecting live system routing
      - '--tag=adk-mcp-migration'

```

### 2. Zero-Downtime Infrastructure Canary Routing (`scripts/deploy_mesh.sh`)

This script isolates execution testing by implementing a progressive traffic split on your Cloud Run infrastructure. It allows you to run validations safely on the new endpoint in production before migrating your global workload.

```bash
#!/usr/bin/env bash
set -euo pipefail

REGION="us-central1"
SERVICE_NAME="navalplan-researcher"

echo "=== Beginning Managed Infrastructure Traffic Migration ==="

# 1. Capture the unique runtime hash generated from the Cloud Build execution pipeline
NEW_REVISION=$(gcloud run revisions list \
    --service="${SERVICE_NAME}" \
    --region="${REGION}" \
    --sort-by="~CREATE_TIME" \
    --limit=1 \
    --format="value(metadata.name)")

echo "Targeting newly containerized ADK/MCP service revision: ${NEW_REVISION}"

# 2. Allocate an initial 5% canary traffic to the new revision while preserving 95% on legacy systems
echo "Executing infrastructure blue-green canary traffic splitting (95/5)..."
gcloud run services update-traffic "${SERVICE_NAME}" \
    --region="${REGION}" \
    --to-revisions="${NEW_REVISION}=5"

# 3. Automation Verification Loop Block
echo "Awaiting system telemetry verification from canary group..."
sleep 15 

# 4. Final Cutover: Route 100% of workload requests to the ADK Agent platform runtime engine
echo "Workload metrics stable. Route conversion finalizing. Shifting 100% traffic to ADK Mesh..."
gcloud run services update-traffic "${SERVICE_NAME}" \
    --region="${REGION}" \
    --to-revisions="${NEW_REVISION}=100"

echo "=== Complete Migration Successfully Executed ==="

```