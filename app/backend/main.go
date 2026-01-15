package main

import (
	"context"
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
		log.SetFormatter(log.JSONFormatter)
	}

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
