package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/log"
)

type Config struct {
	Env string

	// Server
	Port       string
	ContentDir string

	// Database
	DatabaseDSN string

	// Auth
	GoogleClientID     string
	GoogleClientSecret string
	BaseURL            string

	// External Services
	NavalPlanAgentURL string
	GoogleMapsAPIKey  string

	// System
	SystemAPIKey string

	// Logging
	ObscuredDSN string
}

// New reads configuration from environment variables using the provided getEnv function.
func New(getEnv func(string) string) (*Config, error) {

	if getEnv("NAVALPLAN_OA_CLIENT") == "" || getEnv("NAVALPLAN_OA_SECRET") == "" {
		return nil, errors.New("NAVALPLAN_OA_CLIENT or NAVALPLAN_OA_SECRET is not set. Authentication is required.")
	}

	if getEnv("NAVALPLAN_SYSTEM_KEY") == "" {
		return nil, errors.New("NAVALPLAN_SYSTEM_KEY is not set. System API Key is required.")
	}

	if getEnv("NAVALPLAN_BACKEND_MAPS_API_KEY") == "" {
		return nil, errors.New("NAVALPLAN_BACKEND_MAPS_API_KEY is not set. Google Maps API Key is required.")
	}

	// 1. Basic Configuration
	port := getEnv("PORT")
	if port == "" {
		port = getEnv("NAVALPLAN_PORT")
	}
	if port == "" {
		port = "8080"
	}

	// Content Dir Logic: Env > Default
	contentDir := getEnv("NAVALPLAN_CONTENT_DIR")
	if contentDir == "" {
		contentDir = "./static.min"
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
		q := make(url.Values)
		q.Set("host", dbSocket)
		q.Set("sslmode", dbMode)

		u := url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(dbUser, dbPass),
			Path:     "/" + dbName,
			RawQuery: q.Encode(),
		}
		dsn = u.String()
	} else {
		q := make(url.Values)
		q.Set("sslmode", dbMode)

		u := url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(dbUser, dbPass),
			Host:     fmt.Sprintf("%s:%s", dbHost, dbPort),
			Path:     dbName,
			RawQuery: q.Encode(),
		}
		dsn = u.String()
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

	result := &Config{
		Env:                getEnv("ENV"),
		Port:               port,
		ContentDir:         contentDir,
		DatabaseDSN:        dsn,
		GoogleClientID:     getEnv("NAVALPLAN_OA_CLIENT"),
		GoogleClientSecret: getEnv("NAVALPLAN_OA_SECRET"),
		BaseURL:            baseURL,
		NavalPlanAgentURL:  agentURL,
		SystemAPIKey:       getEnv("NAVALPLAN_SYSTEM_KEY"),
		GoogleMapsAPIKey:   getEnv("NAVALPLAN_BACKEND_MAPS_API_KEY"),
	}

	result.ObscuredDSN = ObscureString(dsn, dbPass)

	log.Info("config", "Env", result.Env)
	log.Info("config", "Port", result.Port)
	log.Info("config", "ContentDir", result.ContentDir)
	log.Info("config", "DatabaseDSN", result.ObscuredDSN)
	log.Info("config", "GoogleClientID", result.GoogleClientID)
	log.Info("config", "GoogleClientSecret", ObscureString(result.GoogleClientSecret))
	log.Info("config", "BaseURL", result.BaseURL)
	log.Info("config", "NavalPlanAgentURL", result.NavalPlanAgentURL)
	log.Info("config", "SystemAPIKey", ObscureString(result.SystemAPIKey))
	log.Info("config", "GoogleMapsAPIKey", ObscureString(result.GoogleMapsAPIKey))

	return result, nil
}

// ObscureString replaces the input string with asterisks of the same length.
func ObscureString(input ...string) string {
	if len(input) == 0 {
		return ""
	}
	toObscure := input[0]
	if len(input) > 1 {
		toObscure = input[1]
	}

	if toObscure == "" {
		return input[0]
	}

	// The number of runes (characters) in the input determines the length of the output.
	count := utf8.RuneCountInString(toObscure)
	str := strings.Repeat("*", count)
	return strings.ReplaceAll(input[0], toObscure, str)
}
