package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tpryan/navalplan/services/researcher/config"
)

func TestCreateResearcherAgent(t *testing.T) {
	// Use a mock model if possible, or just check configuration
	// For now, let's see if it instantiates without error (requires API key if real)

	modelName := "gemini-2.0-flash-001"
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping agent creation test because GEMINI_API_KEY is not set")
	}

	mapsKey := os.Getenv("MAPS_API_KEY")
	if mapsKey == "" {
		mapsKey = "dummy-key"
	}

	srv := &Server{
		config: &config.Config{
			ModelName:    modelName,
			GeminiAPIKey: apiKey,
			MapsAPIKey:   mapsKey,
		},
		timings: make(map[string]time.Time),
	}

	researcherTools, err := srv.setupTools()
	if err != nil {
		t.Fatalf("Failed to setup tools: %v", err)
	}

	a, err := srv.createStopAgent(context.Background(), researcherTools)
	if err != nil {
		t.Fatalf("Failed to create researcher agent: %v", err)
	}

	if a.Name() != "researcher_agent" {
		t.Errorf("Expected agent name researcher_agent, got %s", a.Name())
	}
}
