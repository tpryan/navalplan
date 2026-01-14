package tools

import (
	"testing"
)

func TestNewPlacesTool(t *testing.T) {
	tool, err := NewPlacesTool("dummy-key")
	if err != nil {
		t.Fatalf("NewPlacesTool() error = %v", err)
	}

	if tool.Name() != "find_places_nearby" {
		t.Errorf("NewPlacesTool().Name() = %v, want %v", tool.Name(), "find_places_nearby")
	}

	expectedDesc := "Finds places (e.g. marinas, restaurants) near a location using Google Maps Text Search. Returns specific locations with Lat/Lng."
	if tool.Description() != expectedDesc {
		t.Errorf("NewPlacesTool().Description() = %v, want %v", tool.Description(), expectedDesc)
	}
}
