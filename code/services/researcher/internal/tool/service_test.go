package tool

import "testing"

func TestNauticalToolService_AsTools(t *testing.T) {
	// AsTools only wraps method values as ADK function tools; it doesn't
	// invoke any of them, so a zero-value service (no real providers wired
	// up) is enough to exercise the wrapping/schema-building logic.
	svc := &NauticalToolService{}

	got, err := svc.AsTools()
	if err != nil {
		t.Fatalf("AsTools() error = %v", err)
	}

	wantNames := []string{"GetTides", "GetWeather", "GetSunriseSunset", "FindPlacesNearby", "GetSafetyAlerts", "QuerySailingDirections", "QueryCoastPilot"}
	if len(got) != len(wantNames) {
		t.Fatalf("AsTools() returned %d tools, want %d", len(got), len(wantNames))
	}

	for i, name := range wantNames {
		if got[i].Name() != name {
			t.Errorf("tool[%d].Name() = %q, want %q", i, got[i].Name(), name)
		}
		if got[i].Description() == "" {
			t.Errorf("tool %q has an empty description", name)
		}
	}
}
