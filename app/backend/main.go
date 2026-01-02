package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	// We ignore errors because it's okay if one of them is missing, as long as we get the config we need.
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")

	contentDir := flag.String("content", "./static.min", "Path to static content to serve")
	flag.Parse()

	if os.Getenv("NAVALPLAN_OA_CLIENT") == "" || os.Getenv("NAVALPLAN_OA_SECRET") == "" {
		log.Warn("NAVALPLAN_OA_CLIENT or NAVALPLAN_OA_SECRET is not set. Authentication will fail.")
	}

	cfg := loadConfig(os.Getenv, *contentDir)

	if err := run(context.Background(), cfg); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}

func loadConfig(getEnv func(string) string, contentDir string) *config.Config {
	// 1. Basic Configuration
	port := getEnv("PORT")
	if port == "" {
		port = getEnv("NAVALPLAN_PORT")
	}
	if port == "" {
		port = "8080"
	}

	// Construct DSN from components
	dbUser := getEnv("NAVALPLAN_DB_USER")
	dbPass := getEnv("NAVALPLAN_DB_PASS")
	dbHost := getEnv("NAVALPLAN_DB_HOST")
	dbPort := getEnv("NAVALPLAN_DB_PORT")
	dbName := getEnv("NAVALPLAN_DB_NAME")
	dbMode := getEnv("NAVALPLAN_DB_MODE")     // Maps to sslmode
	dbSocket := getEnv("NAVALPLAN_DB_SOCKET") // For Cloud Run / Unix Sockets

	if dbUser == "" {
		dbUser = "navalplan_user"
	}
	if dbPass == "" {
		dbPass = "navalplan_pass"
	}
	if dbHost == "" {
		dbHost = "localhost"
	}
	if dbPort == "" {
		dbPort = "5433"
	}
	if dbName == "" {
		dbName = "navalplan"
	}
	if dbMode == "" {
		dbMode = "disable"
	}

	var dsn string
	if dbSocket != "" {
		dsn = fmt.Sprintf("postgres://%s:%s@/%s?host=%s&sslmode=%s", dbUser, dbPass, dbName, dbSocket, dbMode)
	} else {
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", dbUser, dbPass, dbHost, dbPort, dbName, dbMode)
	}

	// Allow override
	if val := getEnv("NAVALPLAN_DATABASE_URL"); val != "" {
		dsn = val
	}

	baseURL := getEnv("NAVALPLAN_BASE_URL")
	if baseURL == "" {
		// Fallback to construction from GOOGLE_REDIRECT_URL if available
		fullRedirect := getEnv("GOOGLE_REDIRECT_URL")
		if fullRedirect != "" {
			baseURL = strings.TrimSuffix(fullRedirect, "/auth/google/callback")
		}
	}
	if baseURL == "" {
		baseURL = "http://localhost:" + port
	}

	agentURL := getEnv("NAVALPLAN_AGENT_URL")
	if agentURL == "" {
		agentURL = "http://127.0.0.1:8081"
	}

	result := &config.Config{
		Env:                getEnv("ENV"),
		Port:               port,
		ContentDir:         contentDir,
		DatabaseDSN:        dsn,
		GoogleClientID:     getEnv("NAVALPLAN_OA_CLIENT"),
		GoogleClientSecret: getEnv("NAVALPLAN_OA_SECRET"),
		BaseURL:            baseURL,
		NavalPlanAgentURL:  agentURL,
	}

	logdsn := ObscureString(dsn, dbPass)
	logSecret := ObscureString(result.GoogleClientSecret, result.GoogleClientSecret)

	log.Info("config", "Env", result.Env)
	log.Info("config", "Port", result.Port)
	log.Info("config", "ContentDir", result.ContentDir)
	log.Info("config", "DatabaseDSN", logdsn)
	log.Info("config", "GoogleClientID", result.GoogleClientID)
	log.Info("config", "GoogleClientSecret", logSecret)
	log.Info("config", "BaseURL", result.BaseURL)
	log.Info("config", "NavalPlanAgentURL", result.NavalPlanAgentURL)

	return result

}

// ObscureString replaces the input string with asterisks of the same length.
func ObscureString(input, toObscure string) string {
	// The number of runes (characters) in the input determines the length of the output.
	str := strings.Repeat("*", len(toObscure))
	return strings.ReplaceAll(input, toObscure, str)

}

func run(ctx context.Context, cfg *config.Config) error {

	// 2. Initialize DB
	db, err := datastore.New(cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 3. Initialize Server
	srv, err := server.New(db, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}

	// 3.5 Register Routes for static content
	srv.Routes(cfg.ContentDir)

	// 4. Start HTTP Server
	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: srv.Router,
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
