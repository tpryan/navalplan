package agent

import (
	"testing"

	"github.com/tpryan/navalplan/services/researcher/internal/config"
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

	// Pilot and Commodore keep the RAG tools directly and do not use the sub-agent.
	for _, directRAGAgent := range []string{Pilot, Commodore} {
		ag := byName[directRAGAgent]
		if ag.includeHydrographicPilot {
			t.Errorf("%s: includeHydrographicPilot = true, want false", directRAGAgent)
		}
		var hasSailing, hasCoastPilot bool
		for _, tn := range ag.toolNames {
			if tn == toolQuerySailingDirections {
				hasSailing = true
			}
			if tn == toolQueryCoastPilot {
				hasCoastPilot = true
			}
		}
		if !hasSailing || !hasCoastPilot {
			t.Errorf("%s: expected direct RAG tools (%q=%v, %q=%v)", directRAGAgent, toolQuerySailingDirections, hasSailing, toolQueryCoastPilot, hasCoastPilot)
		}
	}
}

func TestCalculateHeadingScopedToLookout(t *testing.T) {
	for _, sp := range specs {
		var hasHeading bool
		for _, tn := range sp.toolNames {
			if tn == toolCalculateHeading {
				hasHeading = true
			}
		}
		want := sp.name == Lookout
		if hasHeading != want {
			t.Errorf("%s: has %q = %v, want %v", sp.name, toolCalculateHeading, hasHeading, want)
		}
	}
}

func TestSupportsThinkingBudget(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gemini-3.5-flash-lite", true},
		{"gemini-3.8-flash", true},
		{"gemini-2.5-flash", true},
		{"gemini-4-argon", false},
		{"gemini-4.0-pro", false},
		{"claude-sonnet-4-5", false},
	}
	for _, tc := range tests {
		if got := supportsThinkingBudget(tc.model); got != tc.want {
			t.Errorf("supportsThinkingBudget(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestEvalOrderAndModelResolution(t *testing.T) {
	if len(EvalOrder) != 8 {
		t.Fatalf("len(EvalOrder) = %d, want 8", len(EvalOrder))
	}
	cfg := &config.Config{
		ModelName:          "gemini-3.5-flash-lite",
		CommodoreModelName: "gemini-4-argon",
		LookoutModelName:   "gemini-2.5-flash",
	}
	if got := cfg.ModelForAgent(Commodore); got != "gemini-4-argon" {
		t.Errorf("ModelForAgent(Commodore) = %q, want gemini-4-argon", got)
	}
	if got := cfg.ModelForAgent(Lookout); got != "gemini-2.5-flash" {
		t.Errorf("ModelForAgent(Lookout) = %q, want gemini-2.5-flash", got)
	}
	if got := cfg.ModelForAgent(FacilityDiscovery); got != "gemini-3.5-flash-lite" {
		t.Errorf("ModelForAgent(FacilityDiscovery) = %q, want gemini-3.5-flash-lite", got)
	}
	cfg.SetUniformModel("gemini-2.5-pro")
	for _, name := range EvalOrder {
		if got := cfg.ModelForAgent(name); got != "gemini-2.5-pro" {
			t.Errorf("after SetUniformModel, ModelForAgent(%q) = %q, want gemini-2.5-pro", name, got)
		}
	}
	cfg.SetUniformModel("claude-sonnet-4-5")
	for _, name := range EvalOrder {
		if got := cfg.ModelForAgent(name); got != "claude-sonnet-4-5" {
			t.Errorf("after SetUniformModel(claude), ModelForAgent(%q) = %q, want claude-sonnet-4-5", name, got)
		}
	}
	if got := cfg.ModelForAgent(Harbourmaster + "_search_specialist"); got != "gemini-3.5-flash-lite" {
		t.Errorf("search specialist model with uniform claude = %q, want gemini-3.5-flash-lite", got)
	}
}
