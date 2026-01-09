package main

import (
	"context"
	"os"
	"testing"

	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

func TestCreateResearcherAgent(t *testing.T) {
	ctx := context.Background()
	// Use a mock model if possible, or just check configuration
	// For now, let's see if it instantiates without error (requires API key if real)

	modelName := "gemini-2.0-flash-001"
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping agent creation test because GEMINI_API_KEY is not set")
	}

	model, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	a, err := CreateResearcherAgent(model)
	if err != nil {
		t.Fatalf("Failed to create researcher agent: %v", err)
	}

	if a.Name() != "researcher_agent" {
		t.Errorf("Expected agent name researcher_agent, got %s", a.Name())
	}
}
