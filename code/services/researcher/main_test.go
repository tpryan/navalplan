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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	researcherTools, err := srv.setupTools(ctx)
	if err != nil {
		t.Fatalf("Failed to setup tools: %v", err)
	}

	a, err := srv.createHarbourmasterAgent(ctx, researcherTools)
	if err != nil {
		t.Fatalf("Failed to create harbourmaster agent: %v", err)
	}

	if a.Name() != "harbourmaster" {
		t.Errorf("Expected agent name harbourmaster, got %s", a.Name())
	}
}
