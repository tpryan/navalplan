package config

import (
	"fmt"
)

type Config struct {
	Env          string
	Project      string
	ModelName    string
	GeminiAPIKey string
	MapsAPIKey   string
	Port         string
}

func New(getEnv func(string) string) (*Config, error) {
	mapsKey := getEnv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if mapsKey == "" {
		return nil, fmt.Errorf("NAVALPLAN_BACKEND_MAPS_API_KEY is not set")
	}

	modelName := getEnv("NAVALPLAN_AGENT_MODEL")
	if modelName == "" {
		modelName = "gemini-2.0-flash-001"
	}

	geminiKey := getEnv("GEMINI_API_KEY")
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

	env := getEnv("ENV")
	if env == "" {
		env = "development"
	}

	project := getEnv("GOOGLE_CLOUD_PROJECT")

	cfg := &Config{
		Env:          env,
		Project:      project,
		ModelName:    modelName,
		GeminiAPIKey: geminiKey,
		MapsAPIKey:   mapsKey,
		Port:         port,
	}

	return cfg, nil
}
