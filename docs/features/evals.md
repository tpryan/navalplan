Here is the implementation plan for integrating the Gemini CLI and agentic evaluations into the `navalplan` repository, drawing directly from the patterns established in the EtchWake project.

### Phase 1: Enable A2A and Agent Card in Go
To allow the Gemini CLI (`adk eval`) to communicate with your agent remotely, you need to expose an Agent Card and a JSON-RPC endpoint. 

Update `services/researcher/main.go` to include the `a2a-go` server handlers:

```go
import (
	// ... existing imports
	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"google.golang.org/adk/server/adka2a"
)

func main() {
    // ... agent initialization ...
    
    // 1. A2A Setup
    agentPath := "/invoke"
    agentCard := &a2a.AgentCard{
        Name:               yourAgent.Name(), // Replace with your actual agent instance
        Skills:             adka2a.BuildAgentSkills(yourAgent),
        PreferredTransport: a2a.TransportProtocolJSONRPC,
        URL:                "http://localhost:" + cfg.Port + agentPath,
        Capabilities:       a2a.AgentCapabilities{Streaming: true},
        DefaultInputModes:  []string{},
        DefaultOutputModes: []string{},
    }

    executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
        RunnerConfig: runner.Config{
            AppName:        yourAgent.Name(),
            Agent:          yourAgent,
            SessionService: launcherConfig.SessionService,
        },
    })
    requestHandler := a2asrv.NewHandler(executor)

    // 2. Setup Router
    mux := http.NewServeMux()
    // ... existing routes ...
    mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))
    mux.Handle(agentPath, a2asrv.NewJSONRPCHandler(requestHandler))
    
    // ... start server ...
}
```

### Phase 2: Evaluation Configuration and Datasets
Set up the standard `adk` testing structure within the researcher service:

1.  **Create Evaluation Directory**: Create `services/researcher/eval/`.
2.  **Define Evaluation Criteria**: Create `services/researcher/eval/test_config.json` with your thresholds (e.g., `response_match_score` at 0.8).
3.  **Bootstrap Golden Dataset**: Create `services/researcher/eval/researcher.test.json` with your golden test cases, ensuring each has an `eval_id`, `session_input`, and `expected_output`.

### Phase 3: Local Workflow Integration (Makefile)
Update the `Makefile` to script the creation of the Python bridging files, start the local server, run the evaluation, and output the data directly to the `.adk` directory in your project root.

```makefile
.PHONY: test-agent-eval
test-agent-eval:
	@echo "Starting Researcher Agent for evaluation..."
	@mkdir -p .adk
	@ln -sf $$(pwd)/.env services/researcher/.env
	@echo "from . import agent" > services/researcher/__init__.py
	@echo "from google.adk.agents import remote_a2a_agent" > services/researcher/agent.py
	@echo "agent = remote_a2a_agent.RemoteA2aAgent(name='researcher_agent', agent_card='http://localhost:8081/.well-known/agent-card.json')" >> services/researcher/agent.py
	@echo "root_agent = agent" >> services/researcher/agent.py
	@(cd services/researcher && go run .) & \
	echo $$! > agent.pid; \
	echo "Waiting for agent to start..."; \
	sleep 10; \
	if ! lsof -i :8081 > /dev/null; then \
		echo "Error: Researcher Agent failed to start on port 8081"; \
		kill $$(cat agent.pid) 2>/dev/null || true; \
		rm agent.pid; \
		exit 1; \
	fi; \
	adk eval services/researcher services/researcher/eval/researcher.test.json --config_file_path=services/researcher/eval/test_config.json --output_dir=$(PWD)/.adk --print_detailed_results; \
	EXIT_CODE=$$?; \
	lsof -ti :8081 | xargs kill -9 2>/dev/null || true; \
	rm -f agent.pid; \
	rm -f services/researcher/.env; \
	rm -f services/researcher/__init__.py services/researcher/agent.py; \
	exit $$EXIT_CODE
```
*Note: Ensure your `test` target in the `Makefile` runs `test-agent-eval` alongside existing tests.*

### Phase 4: CI/CD Pipeline Enforcement
Incorporate the validation step into Cloud Build:

1.  **Update `cloudbuild.yaml`**: Add a test step using a Python-enabled image.
2.  **Logic**: Install `google-adk[a2a]`, inject the `GOOGLE_API_KEY`, and trigger `make test-agent-eval` to block deployments if agent responses drop below the configured threshold.