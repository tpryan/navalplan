// Package server wires the researcher service's agents, tools, and session
// service into an HTTP handler.
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
	"github.com/tpryan/navalplan/services/researcher/internal/agent"
	"github.com/tpryan/navalplan/services/researcher/internal/config"
	"github.com/tpryan/navalplan/services/researcher/internal/mcp"
	"github.com/tpryan/navalplan/services/researcher/internal/telemetry"
	"github.com/tpryan/navalplan/services/researcher/internal/tool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a"
	"google.golang.org/adk/v2/server/adkrest"
	"google.golang.org/adk/v2/session"
)

type Deps struct {
	Config         *config.Config
	Agents         map[string]adkagent.Agent
	SessionService session.Service
	Nautical       *tool.NauticalToolService
	Telemetry      *telemetry.Broadcaster
}

func Build(ctx context.Context, deps Deps) (http.Handler, error) {
	loader, err := adkagent.NewMultiLoader(
		deps.Agents[agent.Harbourmaster],
		deps.Agents[agent.Pilot],
		deps.Agents[agent.Commodore],
		deps.Agents[agent.Specialist],
		deps.Agents[agent.Lookout],
	)
	if err != nil {
		return nil, fmt.Errorf("creating multi loader: %w", err)
	}

	mux := http.NewServeMux()

	mux.Handle("/mcp/tools", mcp.NewHandler(ctx, deps.Nautical))

	harbourmaster := deps.Agents[agent.Harbourmaster]
	registerAgentA2A(mux, deps.Config, harbourmaster, "/invoke", deps.SessionService)
	for _, name := range agent.Order {
		registerAgentA2A(mux, deps.Config, deps.Agents[name], "/invoke/"+name, deps.SessionService)
	}

	agentCard := buildAgentCard(deps.Config, harbourmaster, "/invoke")
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	adkHandler, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:     loader,
		SessionService:  deps.SessionService,
		SSEWriteTimeout: 600 * time.Second,
		DebugConfig:     adkrest.DebugTelemetryConfig{},
	})
	if err != nil {
		return nil, fmt.Errorf("creating ADK server: %w", err)
	}

	mux.Handle("/api/", http.StripPrefix("/api", adkHandler))

	reHandlers := &reasoningEngineHandlers{
		loader:  loader,
		session: deps.SessionService,
	}
	mux.HandleFunc("/api/reasoning_engine", reHandlers.handle)
	mux.HandleFunc("/api/stream_reasoning_engine", reHandlers.handleStream)

	mux.Handle("/telemetry", deps.Telemetry)

	handler := otelhttp.NewHandler(loggingMiddleware(mux), "navalplan-researcher")
	return traceMiddleware(deps.Config.Project, handler), nil
}

func Serve(ctx context.Context, handler http.Handler, addr string, shutdownTimeout time.Duration) error {
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

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

func registerAgentA2A(mux *http.ServeMux, cfg *config.Config, a adkagent.Agent, path string, sessionService session.Service) {
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

func buildAgentCard(cfg *config.Config, a adkagent.Agent, path string) *a2a.AgentCard {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:" + cfg.Port
	}
	return &a2a.AgentCard{
		ProtocolVersion:    "0.3.0",
		Name:               a.Name(),
		Skills:             adka2a.BuildAgentSkills(a),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		URL:                baseURL + path,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		DefaultInputModes:  []string{},
		DefaultOutputModes: []string{},
	}
}
