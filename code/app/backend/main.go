package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"app/internal/config"
	"app/internal/server"
	"app/internal/store"
	"app/internal/telemetry"

	"github.com/charmbracelet/lipgloss"
	charm "github.com/charmbracelet/log"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file (try current dir, then project root)
	godotenv.Load(".env")
	godotenv.Load("../../.env")
	godotenv.Load("../../../.env")

	cfg, err := config.New(os.Getenv)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	var handler slog.Handler

	if cfg.Env == "production" {
		// Production: JSON with Severity mapping
		jsonHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			AddSource: true,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.MessageKey {
					a.Key = "message"
				} else if a.Key == slog.SourceKey {
					a.Key = "logging.googleapis.com/sourceLocation"
				} else if a.Key == slog.LevelKey {
					a.Key = "severity"
				}
				return a
			},
		})
		handler = &server.CloudLoggingHandler{Handler: jsonHandler, FormatMessage: true}
	} else {
		// Development: Charmbracelet colorful slog
		lipgloss.SetHasDarkBackground(true)
		chOptions := charm.Options{Prefix: "backend", ReportTimestamp: true}
		cbLogger := charm.NewWithOptions(os.Stderr, chOptions)
		handler = &server.CloudLoggingHandler{Handler: cbLogger}
	}

	slog.SetDefault(slog.New(handler))

	// Initialize OpenTelemetry
	telemetryCtx, telemetryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer telemetryCancel()
	tp, err := telemetry.InitTelemetry(telemetryCtx, cfg.Project, cfg.Env, cfg.DisableTracing)
	if err != nil {
		slog.Error("Failed to initialize telemetry", "error", err)
	}
	if tp != nil {
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				slog.Error("Failed to shutdown tracer provider", "error", err)
			}
		}()
	}

	slog.Info("config", "Env", cfg.Env)
	slog.Info("config", "Port", cfg.Port)
	slog.Info("config", "ContentDir", cfg.ContentDir)
	slog.Info("config", "DatabaseDSN", cfg.ObscuredDSN)
	slog.Info("config", "GoogleClientID", cfg.GoogleClientID)
	slog.Info("config", "GoogleClientSecret", config.ObscureString(cfg.GoogleClientSecret))
	slog.Info("config", "BaseURL", cfg.BaseURL)
	slog.Info("config", "NavalPlanAgentURL", cfg.NavalPlanAgentURL)
	slog.Info("config", "SystemAPIKey", config.ObscureString(cfg.SystemAPIKey))
	slog.Info("config", "GoogleMapsAPIKey", config.ObscureString(cfg.GoogleMapsAPIKey))

	if err := run(context.Background(), cfg); err != nil {
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config) error {

	// 2. Initialize DB
	db, err := store.New(cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("connecting to database (%s): %w", cfg.ObscuredDSN, err)
	}
	defer db.Close()

	// 3. Initialize Server
	srv, err := server.New(db, cfg)
	if err != nil {
		return fmt.Errorf("initializing server: %w", err)
	}

	// 3.5 Register Routes
	srv.Routes(cfg.ContentDir)

	// 4. Start HTTP Server
	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: srv.Middleware(srv.Mux),
	}

	errChan := make(chan error, 1)
	go func() {
		slog.Info(fmt.Sprintf("NavalPlan starting on port %s...", cfg.Port))
		slog.Info(fmt.Sprintf("Serving static content from: %s", cfg.ContentDir))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()

	// Wait for interruption or context cancellation
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		slog.Info("Shutting down server...")
	case <-ctx.Done():
		slog.Info("Context cancelled, shutting down...")
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	slog.Info("Server exiting")
	return nil
}
