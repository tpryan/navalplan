// Package main is the entry point for the researcher service, orchestrating multiple AI agents
// (Researcher, Guide, Discovery) to assist with sailing voyage planning.
//
// main is a composition root only: it loads configuration, builds the
// service's dependency graph (nautical tools, agents, session service), and
// hands the result to the server package to serve over HTTP. Agent
// construction lives in package agents, HTTP routing/middleware in package
// server, and tool-call/SSE instrumentation in package telemetry.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/tpryan/navalplan/services/researcher/agents"
	"github.com/tpryan/navalplan/services/researcher/config"
	"github.com/tpryan/navalplan/services/researcher/logging"
	"github.com/tpryan/navalplan/services/researcher/prompts"
	"github.com/tpryan/navalplan/services/researcher/server"
	"github.com/tpryan/navalplan/services/researcher/sessions"
	"github.com/tpryan/navalplan/services/researcher/telemetry"
	"github.com/tpryan/navalplan/services/researcher/tools"
	"github.com/tpryan/navalplan/services/researcher/tracing"
	"google.golang.org/adk/v2/session"
)

func main() {
	// Load .env file (try current dir, then project root)
	godotenv.Load(".env")
	godotenv.Load("../../.env")

	cfg, err := config.New(os.Getenv)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	logging.InitLogging(cfg.Env)
	logConfig(cfg)

	ctx := context.Background()

	// Initialize OpenTelemetry
	tp, err := tracing.Init(ctx, cfg.Project, cfg.Env, cfg.DisableTracing)
	if err != nil {
		slog.Error("Failed to initialize telemetry", "error", err)
		// We continue anyway, as telemetry is not critical for service operation
	} else if tp != nil {
		slog.Info("Telemetry initialized successfully")
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				slog.Error("Failed to shutdown tracer provider", "error", err)
			}
		}()
	} else {
		slog.Info("Telemetry was not initialized (likely disabled or not in production)")
	}

	// Validate embedded prompts before attempting agent creation so that an
	// accidentally empty file fails fast with a clear message.
	if err := prompts.Validate(); err != nil {
		slog.Error("Invalid prompts", "error", err)
		os.Exit(1)
	}

	// Use a bounded context for agent/model initialisation. If the Gemini API
	// is unresponsive during startup we fail fast rather than hanging forever.
	initCtx, initCancel := context.WithTimeout(ctx, 30*time.Second)
	defer initCancel()

	nautical, providers, err := tools.NewNauticalService(initCtx, cfg.MapsAPIKey, cfg.UKTidalAPIKey, cfg.NIWAAPIKey)
	if err != nil {
		slog.Error("Failed to set up nautical service", "error", err)
		os.Exit(1)
	}
	defer closeProviders(providers)

	researcherTools, err := nautical.AsTools()
	if err != nil {
		slog.Error("Failed to build researcher tools", "error", err)
		os.Exit(1)
	}

	broadcaster := telemetry.NewBroadcaster()
	tracker := telemetry.NewToolTracker(broadcaster)

	factory := agents.NewFactory(cfg, tracker.BeforeTool, tracker.AfterTool)
	builtAgents, err := factory.BuildAll(initCtx, researcherTools)
	if err != nil {
		slog.Error("Failed to build agents", "error", err)
		os.Exit(1)
	}

	handler, err := server.Build(ctx, server.Deps{
		Config:         cfg,
		Agents:         builtAgents,
		SessionService: sessions.NewAutoCreate(session.InMemoryService()),
		Nautical:       nautical,
		Telemetry:      broadcaster,
	})
	if err != nil {
		slog.Error("Failed to build HTTP handler", "error", err)
		os.Exit(1)
	}

	// Cloud Run gives instances a limited window to drain in-flight requests
	// on scale-down; keep that bounded, but don't make local dev wait 5 minutes.
	shutdownTimeout := 5 * time.Minute
	if cfg.Env == "development" {
		shutdownTimeout = 5 * time.Second
	}

	if err := server.Serve(ctx, handler, ":"+cfg.Port, shutdownTimeout); err != nil {
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}

func logConfig(cfg *config.Config) {
	slog.Info("config", "modelName", cfg.ModelName)
	slog.Info("config", "port", cfg.Port)
	if len(cfg.MapsAPIKey) > 5 {
		slog.Info("config", "MapsAPIKey", cfg.MapsAPIKey[:5]+"...")
	}
	if len(cfg.UKTidalAPIKey) > 5 {
		slog.Info("config", "UKTidalAPIKey", cfg.UKTidalAPIKey[:5]+"...")
	}
	if len(cfg.NIWAAPIKey) > 5 {
		slog.Info("config", "NIWAAPIKey", cfg.NIWAAPIKey[:5]+"...")
	}
}

// closeProviders releases every nautical-tool provider on shutdown, logging
// (rather than failing) individual close errors so one bad provider can't
// block the others from cleaning up.
func closeProviders(providers []tools.Provider) {
	var failed int
	for _, p := range providers {
		if err := p.Close(); err != nil {
			slog.Error("Failed to close provider", "error", err)
			failed++
		}
	}
	if failed > 0 {
		slog.Warn("Some providers failed to close cleanly", "count", failed)
	}
}
