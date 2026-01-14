package main

import (
	"os"
	"sync"
	"testing"
)

func TestCreateResearcherAgent(t *testing.T) {
	// Use a mock model if possible, or just check configuration
	// For now, let's see if it instantiates without error (requires API key if real)

	modelName := "gemini-2.0-flash-001"
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping agent creation test because GEMINI_API_KEY is not set")
	}

	srv := &Server{
		modelName: modelName,
		apiKey:    apiKey,
		timings:   sync.Map{},
	}

	a, err := srv.createResearcherAgent()
	if err != nil {
		t.Fatalf("Failed to create researcher agent: %v", err)
	}

	if a.Name() != "researcher_agent" {
		t.Errorf("Expected agent name researcher_agent, got %s", a.Name())
	}
}
