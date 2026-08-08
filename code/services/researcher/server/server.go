// Package server wires the researcher service's agents, tools, and session
// service into an HTTP handler (A2A, ADK REST, MCP, Reasoning Engine, and
// telemetry endpoints), and runs it with graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/tpryan/navalplan/services/researcher/agents"
	"github.com/tpryan/navalplan/services/researcher/config"
	"github.com/tpryan/navalplan/services/researcher/mcp"
	"github.com/tpryan/navalplan/services/researcher/telemetry"
	"github.com/tpryan/navalplan/services/researcher/tools"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a"
	"google.golang.org/adk/v2/server/adkrest"
	"google.golang.org/adk/v2/session"
)

// Deps are the fully-constructed dependencies the HTTP handler is built
// from. Everything here is assembled by main; this package only wires it
// into routes and middleware.
type Deps struct {
	Config         *config.Config
	Agents         map[string]agent.Agent
	SessionService session.Service
	Nautical       *tools.NauticalToolService
	Telemetry      *telemetry.Broadcaster
}

// Build assembles the full HTTP handler for the service: the MCP tools
// endpoint, A2A agent endpoints, the well-known agent card, health check,
// the ADK REST API, the native Reasoning Engine contract endpoints, and the
// telemetry SSE feed — wrapped in tracing and request-logging middleware.
func Build(ctx context.Context, deps Deps) (http.Handler, error) {
	loader, err := agent.NewMultiLoader(
		deps.Agents[agents.Harbourmaster],
		deps.Agents[agents.Pilot],
		deps.Agents[agents.Commodore],
		deps.Agents[agents.Specialist],
		deps.Agents[agents.Lookout],
	)
	if err != nil {
		return nil, fmt.Errorf("creating multi loader: %w", err)
	}

	mux := http.NewServeMux()

	// Mount the MCP server endpoint as recommended in geap.md
	mux.Handle("/mcp/tools", mcp.NewHandler(ctx, deps.Nautical))

	// Expose to a2a for eval purposes. The harbourmaster is additionally
	// registered at the bare "/invoke" path as the default agent.
	harbourmaster := deps.Agents[agents.Harbourmaster]
	registerAgentA2A(mux, deps.Config, harbourmaster, "/invoke", deps.SessionService)
	for _, name := range agents.Order {
		registerAgentA2A(mux, deps.Config, deps.Agents[name], "/invoke/"+name, deps.SessionService)
	}

	// Special case: The root Agent Card at .well-known usually points to the main agent.
	// We'll point it to harbourmaster for now.
	agentCard := buildAgentCard(deps.Config, harbourmaster, "/invoke")
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Create the ADK HTTP Server
	adkHandler, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:     loader,
		SessionService:  deps.SessionService,
		SSEWriteTimeout: 600 * time.Second,
		DebugConfig:     adkrest.DebugTelemetryConfig{},
	})
	if err != nil {
		return nil, fmt.Errorf("creating ADK server: %w", err)
	}

	// Mount ADK under /api/
	mux.Handle("/api/", http.StripPrefix("/api", adkHandler))

	// Native Reasoning Engine contract endpoints
	reHandlers := &reasoningEngineHandlers{
		loader:  loader,
		session: deps.SessionService,
	}
	mux.HandleFunc("/api/reasoning_engine", reHandlers.handle)
	mux.HandleFunc("/api/stream_reasoning_engine", reHandlers.handleStream)

	// Telemetry endpoint — streams internal tool execution events.
	// In production this is protected by Cloud Run IAM; no additional
	// application-level auth is enforced here.
	mux.Handle("/telemetry", deps.Telemetry)

	// Wrap the entire handler with OpenTelemetry and logging middleware
	handler := otelhttp.NewHandler(loggingMiddleware(mux), "navalplan-researcher")
	return traceMiddleware(deps.Config.Project, handler), nil
}

// Serve runs handler on addr until ctx is done or a SIGTERM/SIGINT is
// received, at which point it drains in-flight requests (bounded by
// shutdownTimeout) before returning.
func Serve(ctx context.Context, handler http.Handler, addr string, shutdownTimeout time.Duration) error {
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Listen for SIGTERM/SIGINT so Cloud Run scale-down drains in-flight requests.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-quit:
		case <-ctx.Done():
		}
		slog.Info("Shutdown signal received, draining requests...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", "error", err)
			httpSrv.Close()
		}
	}()

	slog.Info("Starting custom server", "addr", addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func registerAgentA2A(mux *http.ServeMux, cfg *config.Config, a agent.Agent, path string, sessionService session.Service) {
	card := buildAgentCard(cfg, a, path)
	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        a.Name(),
			Agent:          a,
			SessionService: sessionService,
		},
	})
	handler := a2asrv.NewHandler(executor)

	mux.Handle(path, a2asrv.NewJSONRPCHandler(handler))
	mux.Handle(path+"/agent-card.json", a2asrv.NewStaticAgentCardHandler(card))
	slog.Info("Registered A2A agent", "name", a.Name(), "path", path)
}

func buildAgentCard(cfg *config.Config, a agent.Agent, path string) *a2a.AgentCard {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:" + cfg.Port
	}
	return &a2a.AgentCard{
		Name:               a.Name(),
		Skills:             adka2a.BuildAgentSkills(a),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		URL:                baseURL + path,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		DefaultInputModes:  []string{},
		DefaultOutputModes: []string{},
	}
}
