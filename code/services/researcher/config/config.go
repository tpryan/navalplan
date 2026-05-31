package config

import (
	"fmt"
	"strconv"
)

type Config struct {
	Env             string
	Project         string
	ModelName       string
	GeminiAPIKey    string
	MapsAPIKey      string
	UKTidalAPIKey   string
	NIWAAPIKey      string
	Port            string
	BaseURL         string
	ThinkingBudget  int32
	SearchTimeoutMs int
}

func New(getEnv func(string) string) (*Config, error) {
	mapsKey := getEnv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if mapsKey == "" {
		mapsKey = getEnv("GOOGLE_MAPS_API_KEY")
	}
	if mapsKey == "" {
		return nil, fmt.Errorf("NAVALPLAN_BACKEND_MAPS_API_KEY is not set")
	}

	modelName := getEnv("NAVALPLAN_AGENT_MODEL")
	if modelName == "" {
		modelName = "gemini-2.0-flash-001"
	}

	geminiKey := getEnv("GEMINI_API_KEY")
	if geminiKey == "" {
		geminiKey = getEnv("GOOGLE_API_KEY")
	}
	if geminiKey == "" {
		geminiKey = getEnv("NAVALPLAN_GEMINI_KEY")
	}
	if geminiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}

	port := getEnv("PORT")
	if port == "" {
		port = getEnv("NAVALPLAN_AGENT_PORT")
	}
	if port == "" {
		port = "8081"
	}

	baseURL := getEnv("NAVALPLAN_AGENT_BASE_URL")

	ukTidalKey := getEnv("NAVALPLAN_TIDAL_UKTIDAL_API_KEY")
	niwaKey := getEnv("NAVALPLAN_TIDAL_NIWA_API_KEY")

	env := getEnv("ENV")
	if env == "" {
		env = "development"
	}

	project := getEnv("GOOGLE_CLOUD_PROJECT")

	// Thinking budget caps the reasoning tokens the (thinking-capable) Gemini model spends
	// per call. Unbounded "dynamic" thinking is the dominant source of agent latency and can
	// truncate structured output. 0 disables thinking; -1 restores dynamic/default behavior.
	// Default to a modest bound that keeps responses fast without starving synthesis quality.
	thinkingBudget := int32(1024)
	if v := getEnv("NAVALPLAN_AGENT_THINKING_BUDGET"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			thinkingBudget = int32(n)
		}
	}

	// Per-query timeout for grounded web searches, so one slow query can't stall the whole
	// parallel batch (and thus the agent turn). Default 45s; 0 disables the bound.
	searchTimeoutMs := 45000
	if v := getEnv("NAVALPLAN_AGENT_SEARCH_TIMEOUT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			searchTimeoutMs = n
		}
	}

	cfg := &Config{
		Env:             env,
		Project:         project,
		ModelName:       modelName,
		GeminiAPIKey:    geminiKey,
		MapsAPIKey:      mapsKey,
		UKTidalAPIKey:   ukTidalKey,
		NIWAAPIKey:      niwaKey,
		Port:            port,
		BaseURL:         baseURL,
		ThinkingBudget:  thinkingBudget,
		SearchTimeoutMs: searchTimeoutMs,
	}

	return cfg, nil
}
