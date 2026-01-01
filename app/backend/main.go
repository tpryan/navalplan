package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"app/datastore"
	"app/server"

	"github.com/charmbracelet/log"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file (try current dir, then project root)
	// We ignore errors because it's okay if one of them is missing, as long as we get the config we need.
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")

	contentDir := flag.String("content", "./static.min", "Path to static content to serve")
	flag.Parse()

	if os.Getenv("GOOGLE_CLIENT_ID") == "" || os.Getenv("GOOGLE_CLIENT_SECRET") == "" {
		log.Warn("GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET is not set. Authentication will fail.")
	}

	if err := run(context.Background(), os.Stdout, os.Getenv, *contentDir); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, w io.Writer, getEnv func(string) string, contentDir string) error {
	logger := log.New(w)
	logger.SetPrefix("main")

	// 1. Basic Configuration
	port := getEnv("NAVALPLAN_PORT")
	if port == "" {
		port = "8080"
	}

	dsn := getEnv("NAVALPLAN_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://navalplan_user:navalplan_pass@localhost:5433/navalplan?sslmode=disable"
	}

	// 2. Initialize DB
	db, err := datastore.New(dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 3. Initialize Server
	srv, err := server.New(db)
	if err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}

	// 3.5 Register Routes for static content
	srv.Routes(contentDir)

	// 4. Start HTTP Server
	httpServer := &http.Server{
		Addr:    ":" + port,
		Handler: srv.Router,
	}

	errChan := make(chan error, 1)
	go func() {
		log.Infof("NavalPlan starting on port %s...", port)
		log.Infof("Serving static content from: %s", contentDir)
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
