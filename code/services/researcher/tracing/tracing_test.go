package tracing

import (
	"context"
	"testing"
)

// Init's production path talks to real GCP resource-detection and Cloud
// Trace endpoints, so it isn't exercised here. These tests cover the two
// early-return branches that don't require any network access.

func TestInit_DisabledExplicitly(t *testing.T) {
	providers, err := Init(context.Background(), "some-project", "production", true)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if providers != nil {
		t.Errorf("Init() with disableTracing=true should return nil providers, got %v", providers)
	}
}

func TestInit_DisabledOutsideProduction(t *testing.T) {
	for _, env := range []string{"development", "staging", "test", ""} {
		providers, err := Init(context.Background(), "some-project", env, false)
		if err != nil {
			t.Fatalf("Init() env=%q error = %v", env, err)
		}
		if providers != nil {
			t.Errorf("Init() env=%q should return nil providers outside production, got %v", env, providers)
		}
	}
}
