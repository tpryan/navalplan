package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"app/config"
	"app/datastore"
	"app/server"

	"github.com/charmbracelet/log"
	"github.com/joho/godotenv"
)

func main() {
	log.SetPrefix("main")
	// Load .env file (try current dir, then project root)
	err1 := godotenv.Load(".env")
	err2 := godotenv.Load("../../.env")
	if err1 != nil && err2 != nil {
		log.Info("No .env file found, relying on environment variables")
	}

	cfg, err := config.New(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.Env == "production" {
		wrapper := &SeverityWrapper{Outer: os.Stderr}
		log.SetOutput(wrapper)
		log.SetFormatter(log.JSONFormatter)
	}

	log.Info("config", "Env", cfg.Env)
	log.Info("config", "Port", cfg.Port)
	log.Info("config", "ContentDir", cfg.ContentDir)
	log.Info("config", "DatabaseDSN", cfg.ObscuredDSN)
	log.Info("config", "GoogleClientID", cfg.GoogleClientID)
	log.Info("config", "GoogleClientSecret", config.ObscureString(cfg.GoogleClientSecret))
	log.Info("config", "BaseURL", cfg.BaseURL)
	log.Info("config", "NavalPlanAgentURL", cfg.NavalPlanAgentURL)
	log.Info("config", "SystemAPIKey", config.ObscureString(cfg.SystemAPIKey))
	log.Info("config", "GoogleMapsAPIKey", config.ObscureString(cfg.GoogleMapsAPIKey))

	if err := run(context.Background(), cfg); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config) error {

	// 2. Initialize DB
	db, err := datastore.New(cfg.DatabaseDSN)
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
		log.Infof("NavalPlan starting on port %s...", cfg.Port)
		log.Infof("Serving static content from: %s", cfg.ContentDir)
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
		log.Info("Shutting down server...")
	case <-ctx.Done():
		log.Info("Context cancelled, shutting down...")
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	log.Info("Server exiting")
	return nil
}

// SeverityWrapper intercepts the log output to rename the "level" field.
type SeverityWrapper struct {
	Outer *os.File
}

func (w *SeverityWrapper) Write(p []byte) (n int, err error) {
	// 1. Unmarshal the original JSON produced by the logger
	var data map[string]interface{}
	if err := json.Unmarshal(p, &data); err != nil {
		// If it's not valid JSON, just write the original bytes
		return w.Outer.Write(p)
	}

	// 2. Rename the "level" field to "severity"
	if level, ok := data["level"]; ok {
		data["severity"] = level
		delete(data, "level")
	}

	// 3. Re-marshal the modified map
	modifiedJSON, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}

	// 4. Write the modified JSON with a newline
	modifiedJSON = append(modifiedJSON, '\n')
	_, err = w.Outer.Write(modifiedJSON)

	// Return the original length to satisfy io.Writer
	return len(p), err
}
