package agent

import (
	"testing"

	adktool "google.golang.org/adk/v2/tool"
)

// fakeTool is a minimal adktool.Tool used to exercise name-based selection
// without building real function tools (which require schema reflection).
type fakeTool struct{ name string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "fake tool " + f.name }
func (f fakeTool) IsLongRunning() bool { return false }

func TestToolByName(t *testing.T) {
	tools := []adktool.Tool{
		fakeTool{name: toolGetTides},
		fakeTool{name: toolFindPlacesNearby},
		fakeTool{name: toolQuerySailingDirections},
	}

	got, err := toolByName(tools, toolFindPlacesNearby)
	if err != nil {
		t.Fatalf("toolByName(%q) error = %v, want nil", toolFindPlacesNearby, err)
	}
	if got.Name() != toolFindPlacesNearby {
		t.Errorf("toolByName returned %q, want %q", got.Name(), toolFindPlacesNearby)
	}

	if _, err := toolByName(tools, "NotARegisteredTool"); err == nil {
		t.Errorf("toolByName(unknown) error = nil, want non-nil")
	}
}

// TestFacilityDiscoveryDelegation guards the M3 invariant: harbourmaster and
// specialist delegate facility discovery to the facility_discovery sub-agent
// and therefore must NOT hold FindPlacesNearby directly, while no other agent
// accidentally gains the facility delegation.
func TestFacilityDiscoveryDelegation(t *testing.T) {
	byName := make(map[string]spec, len(specs))
	for _, sp := range specs {
		byName[sp.name] = sp
	}

	for _, name := range []string{Harbourmaster, Specialist} {
		sp, ok := byName[name]
		if !ok {
			t.Fatalf("spec %q not found", name)
		}
		if !sp.includeFacilityDiscovery {
			t.Errorf("%s: includeFacilityDiscovery = false, want true", name)
		}
		for _, tn := range sp.toolNames {
			if tn == toolFindPlacesNearby {
				t.Errorf("%s: still holds %q directly; it should delegate to facility_discovery", name, toolFindPlacesNearby)
			}
		}
	}

	// Pilot keeps FindPlacesNearby directly and does not use the sub-agent.
	pilot := byName[Pilot]
	if pilot.includeFacilityDiscovery {
		t.Errorf("pilot: includeFacilityDiscovery = true, want false")
	}
}

// TestHydrographicPilotDelegation guards the M4 invariant: harbourmaster,
// specialist, and lookout delegate hydrographic/RAG retrieval to the
// hydrographic_pilot sub-agent and therefore must NOT hold QuerySailingDirections
// or QueryCoastPilot directly; pilot keeps them directly and does not delegate.
func TestHydrographicPilotDelegation(t *testing.T) {
	byName := make(map[string]spec, len(specs))
	for _, sp := range specs {
		byName[sp.name] = sp
	}

	for _, name := range []string{Harbourmaster, Specialist, Lookout} {
		sp, ok := byName[name]
		if !ok {
			t.Fatalf("spec %q not found", name)
		}
		if !sp.includeHydrographicPilot {
			t.Errorf("%s: includeHydrographicPilot = false, want true", name)
		}
		for _, tn := range sp.toolNames {
			if tn == toolQuerySailingDirections || tn == toolQueryCoastPilot {
				t.Errorf("%s: still holds %q directly; it should delegate to hydrographic_pilot", name, tn)
			}
		}
	}

	// Pilot keeps the RAG tools directly and does not use the sub-agent.
	pilot := byName[Pilot]
	if pilot.includeHydrographicPilot {
		t.Errorf("pilot: includeHydrographicPilot = true, want false")
	}
	var hasSailing bool
	for _, tn := range pilot.toolNames {
		if tn == toolQuerySailingDirections {
			hasSailing = true
		}
	}
	if !hasSailing {
		t.Errorf("pilot: expected to keep %q directly", toolQuerySailingDirections)
	}
}
