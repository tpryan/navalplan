package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tpryan/navalplan/services/researcher/agents"
	"github.com/tpryan/navalplan/services/researcher/config"
	"github.com/tpryan/navalplan/services/researcher/telemetry"
	"github.com/tpryan/navalplan/services/researcher/tools"
)

func TestCreateHarbourmasterAgent(t *testing.T) {
	// Use a mock model if possible, or just check configuration
	// For now, let's see if it instantiates without error (requires API key if real)

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping agent creation test because GEMINI_API_KEY is not set")
	}

	mapsKey := os.Getenv("MAPS_API_KEY")
	if mapsKey == "" {
		mapsKey = "dummy-key"
	}

	cfg := &config.Config{
		ModelName:    "gemini-2.0-flash-001",
		GeminiAPIKey: apiKey,
		MapsAPIKey:   mapsKey,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nautical, providers, err := tools.NewNauticalService(ctx, cfg.MapsAPIKey, cfg.UKTidalAPIKey, cfg.NIWAAPIKey)
	if err != nil {
		t.Fatalf("Failed to set up nautical service: %v", err)
	}
	defer closeProviders(providers)

	researcherTools, err := nautical.AsTools()
	if err != nil {
		t.Fatalf("Failed to build researcher tools: %v", err)
	}

	tracker := telemetry.NewToolTracker(telemetry.NewBroadcaster())
	factory := agents.NewFactory(cfg, tracker.BeforeTool, tracker.AfterTool)

	built, err := factory.BuildAll(ctx, researcherTools)
	if err != nil {
		t.Fatalf("Failed to build agents: %v", err)
	}

	a, ok := built[agents.Harbourmaster]
	if !ok {
		t.Fatalf("Expected %q agent to be built", agents.Harbourmaster)
	}

	if a.Name() != agents.Harbourmaster {
		t.Errorf("Expected agent name %q, got %s", agents.Harbourmaster, a.Name())
	}
}
