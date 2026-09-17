// Package main is the entry point for the researcher service, orchestrating multiple AI agents
// (Researcher, Guide, Discovery) to assist with sailing voyage planning.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/tpryan/navalplan/services/researcher/internal/agent"
	"github.com/tpryan/navalplan/services/researcher/internal/config"
	"github.com/tpryan/navalplan/services/researcher/internal/prompt"
	"github.com/tpryan/navalplan/services/researcher/internal/server"
	"github.com/tpryan/navalplan/services/researcher/internal/session"
	"github.com/tpryan/navalplan/services/researcher/internal/telemetry"
	"github.com/tpryan/navalplan/services/researcher/internal/tool"
	adksession "google.golang.org/adk/v2/session"
)

func main() {
	godotenv.Load(".env")
	godotenv.Load("../../.env")
	godotenv.Load("../../../.env")

	cfg, err := config.New(os.Getenv)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	telemetry.InitLogging(cfg.Env)
	logConfig(cfg)

	ctx := context.Background()

	tp, err := telemetry.InitTracing(ctx, cfg.Project, cfg.Env, cfg.DisableTracing)
	if err != nil {
		slog.Error("Failed to initialize telemetry", "error", err)
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

	if err := prompt.Validate(); err != nil {
		slog.Error("Invalid prompts", "error", err)
		os.Exit(1)
	}

	initCtx, initCancel := context.WithTimeout(ctx, 30*time.Second)
	defer initCancel()

	nautical, providers, err := tool.NewNauticalService(initCtx, cfg.MapsAPIKey, cfg.UKTidalAPIKey, cfg.NIWAAPIKey, cfg.Project, cfg.VertexLocation, cfg.CoastPilotCorpusID, cfg.NGACorpusID)
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

	builtAgents, err := agent.Build(initCtx, cfg, tracker.BeforeTool, tracker.AfterTool, researcherTools)
	if err != nil {
		slog.Error("Failed to build agents", "error", err)
		os.Exit(1)
	}

	handler, err := server.Build(ctx, server.Deps{
		Config:         cfg,
		Agents:         builtAgents,
		SessionService: session.NewAutoCreate(adksession.InMemoryService()),
		Nautical:       nautical,
		Telemetry:      broadcaster,
	})
	if err != nil {
		slog.Error("Failed to build HTTP handler", "error", err)
		os.Exit(1)
	}

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

func closeProviders(providers []tool.Provider) {
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
