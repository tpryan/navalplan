package tool

import (
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestNauticalToolService_AsTools(t *testing.T) {
	// AsTools only wraps method values as ADK function tools; it doesn't
	// invoke any of them, so a zero-value service (no real providers wired
	// up) is enough to exercise the wrapping/schema-building logic.
	svc := &NauticalToolService{}

	got, err := svc.AsTools()
	if err != nil {
		t.Fatalf("AsTools() error = %v", err)
	}

	// FilterGPXData is intentionally no longer wrapped as an LLM tool (GPX
	// smoothing runs deterministically in Go on upload).
	wantNames := []string{"GetTides", "GetWeather", "GetSunriseSunset", "FindPlacesNearby", "GetSafetyAlerts", "QuerySailingDirections", "QueryCoastPilot", "CalculateHeading", "ComputePassageTimeline", "GetCandidateRegionsForMonth"}
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

func TestRequestSchemas_OptionalFieldsNotRequired(t *testing.T) {
	assertSchemaRequired := func(t *testing.T, name string, s *jsonschema.Schema, err error, wantRequired []string, wantOptional []string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s schema error: %v", name, err)
		}
		for _, req := range wantRequired {
			if !slices.Contains(s.Required, req) {
				t.Errorf("%s: expected field %q to be required, got required=%v", name, req, s.Required)
			}
		}
		for _, opt := range wantOptional {
			if slices.Contains(s.Required, opt) {
				t.Errorf("%s: expected field %q to be optional (omitempty), got required=%v", name, opt, s.Required)
			}
		}
	}

	sSafety, err := jsonschema.For[SafetyRequest](nil)
	assertSchemaRequired(t, "SafetyRequest", sSafety, err,
		[]string{"latitude", "longitude"},
		[]string{"region", "radius_nm"},
	)

	sTides, err := jsonschema.For[TideRequest](nil)
	assertSchemaRequired(t, "TideRequest", sTides, err,
		[]string{"latitude", "longitude", "date"},
		[]string{"station_id"},
	)

	sPlaces, err := jsonschema.For[PlacesRequest](nil)
	assertSchemaRequired(t, "PlacesRequest", sPlaces, err,
		[]string{"query", "latitude", "longitude"},
		[]string{"radius"},
	)

	sTimeline, err := jsonschema.For[TimelineRequest](nil)
	assertSchemaRequired(t, "TimelineRequest", sTimeline, err,
		[]string{"from_latitude", "from_longitude", "to_latitude", "to_longitude", "date"},
		[]string{"from_location", "to_location", "planned_distance_nm"},
	)
}
