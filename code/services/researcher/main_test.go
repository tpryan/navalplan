package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tpryan/navalplan/services/researcher/internal/agent"
	"github.com/tpryan/navalplan/services/researcher/internal/config"
	"github.com/tpryan/navalplan/services/researcher/internal/telemetry"
	"github.com/tpryan/navalplan/services/researcher/internal/tool"
)

func TestCreateHarbourmasterAgent(t *testing.T) {
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

	nautical, providers, err := tool.NewNauticalService(ctx, cfg.MapsAPIKey, cfg.UKTidalAPIKey, cfg.NIWAAPIKey)
	if err != nil {
		t.Fatalf("Failed to set up nautical service: %v", err)
	}
	defer closeProviders(providers)

	researcherTools, err := nautical.AsTools()
	if err != nil {
		t.Fatalf("Failed to build researcher tools: %v", err)
	}

	tracker := telemetry.NewToolTracker(telemetry.NewBroadcaster())

	built, err := agent.Build(ctx, cfg, tracker.BeforeTool, tracker.AfterTool, researcherTools)
	if err != nil {
		t.Fatalf("Failed to build agents: %v", err)
	}

	a, ok := built[agent.Harbourmaster]
	if !ok {
		t.Fatalf("Expected %q agent to be built", agent.Harbourmaster)
	}

	if a.Name() != agent.Harbourmaster {
		t.Errorf("Expected agent name %q, got %s", agent.Harbourmaster, a.Name())
	}
}
