package config

import "testing"

func envMap(vars map[string]string) func(string) string {
	return func(key string) string {
		return vars[key]
	}
}

func TestNew_RequiresMapsAPIKey(t *testing.T) {
	_, err := New(envMap(map[string]string{
		"GEMINI_API_KEY": "gemini-key",
	}))
	if err == nil {
		t.Fatal("New() should error when no Maps API key is set")
	}
}

func TestNew_MapsAPIKeyFallback(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"GOOGLE_MAPS_API_KEY": "fallback-maps-key",
		"GEMINI_API_KEY":      "gemini-key",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfg.MapsAPIKey != "fallback-maps-key" {
		t.Errorf("MapsAPIKey = %q, want fallback-maps-key", cfg.MapsAPIKey)
	}
}

func TestNew_RequiresGeminiAPIKey(t *testing.T) {
	_, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
	}))
	if err == nil {
		t.Fatal("New() should error when no Gemini API key is set and GOOGLE_GENAI_USE_VERTEXAI is not enabled")
	}
}

func TestNew_VertexAIModeDoesNotRequireGeminiAPIKey(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
		"GOOGLE_GENAI_USE_VERTEXAI":      "true",
		"GOOGLE_CLOUD_PROJECT":           "navallog",
	}))
	if err != nil {
		t.Fatalf("New() unexpected error with GOOGLE_GENAI_USE_VERTEXAI=true: %v", err)
	}
	if !cfg.UseVertexAI {
		t.Error("UseVertexAI = false, want true")
	}
	if cfg.GeminiLocation != "global" {
		t.Errorf("GeminiLocation = %q, want \"global\"", cfg.GeminiLocation)
	}
}

func TestNew_GeminiAPIKeyFallbackOrder(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{
			name: "prefers GEMINI_API_KEY",
			vars: map[string]string{
				"GEMINI_API_KEY":       "primary",
				"GOOGLE_API_KEY":       "secondary",
				"NAVALPLAN_GEMINI_KEY": "tertiary",
			},
			want: "primary",
		},
		{
			name: "falls back to GOOGLE_API_KEY",
			vars: map[string]string{
				"GOOGLE_API_KEY":       "secondary",
				"NAVALPLAN_GEMINI_KEY": "tertiary",
			},
			want: "secondary",
		},
		{
			name: "falls back to NAVALPLAN_GEMINI_KEY",
			vars: map[string]string{
				"NAVALPLAN_GEMINI_KEY": "tertiary",
			},
			want: "tertiary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]string{"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key"}
			for k, v := range tt.vars {
				vars[k] = v
			}
			cfg, err := New(envMap(vars))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if cfg.GeminiAPIKey != tt.want {
				t.Errorf("GeminiAPIKey = %q, want %q", cfg.GeminiAPIKey, tt.want)
			}
		})
	}
}

func TestNew_Defaults(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
		"GEMINI_API_KEY":                 "gemini-key",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if cfg.Env != "development" {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.Port != "8081" {
		t.Errorf("Port = %q, want 8081", cfg.Port)
	}
	if cfg.ThinkingBudget != 1024 {
		t.Errorf("ThinkingBudget = %d, want 1024", cfg.ThinkingBudget)
	}
	if cfg.SearchTimeoutMs != 30000 {
		t.Errorf("SearchTimeoutMs = %d, want 30000", cfg.SearchTimeoutMs)
	}
	if cfg.DisableTracing {
		t.Error("DisableTracing = true, want false by default")
	}
	if cfg.CommodoreModelName != "gemini-4-argon" {
		t.Errorf("CommodoreModelName = %q, want gemini-4-argon", cfg.CommodoreModelName)
	}
}

func TestNew_PortFallback(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
		"GEMINI_API_KEY":                 "gemini-key",
		"NAVALPLAN_AGENT_PORT":           "9090",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want 9090", cfg.Port)
	}
}

func TestNew_PortPrefersPORT(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
		"GEMINI_API_KEY":                 "gemini-key",
		"PORT":                           "3000",
		"NAVALPLAN_AGENT_PORT":           "9090",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfg.Port != "3000" {
		t.Errorf("Port = %q, want 3000", cfg.Port)
	}
}

func TestNew_ThinkingBudgetOverride(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY":  "maps-key",
		"GEMINI_API_KEY":                  "gemini-key",
		"NAVALPLAN_AGENT_THINKING_BUDGET": "2048",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfg.ThinkingBudget != 2048 {
		t.Errorf("ThinkingBudget = %d, want 2048", cfg.ThinkingBudget)
	}
}

func TestNew_ThinkingBudgetInvalidValueKeepsDefault(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY":  "maps-key",
		"GEMINI_API_KEY":                  "gemini-key",
		"NAVALPLAN_AGENT_THINKING_BUDGET": "not-a-number",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfg.ThinkingBudget != 1024 {
		t.Errorf("ThinkingBudget = %d, want default 1024 when the override fails to parse", cfg.ThinkingBudget)
	}
}

func TestNew_SearchTimeoutOverride(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY":    "maps-key",
		"GEMINI_API_KEY":                    "gemini-key",
		"NAVALPLAN_AGENT_SEARCH_TIMEOUT_MS": "0",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfg.SearchTimeoutMs != 0 {
		t.Errorf("SearchTimeoutMs = %d, want 0 (explicitly disabled)", cfg.SearchTimeoutMs)
	}
}

func TestNew_DisableTracing(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
		"GEMINI_API_KEY":                 "gemini-key",
		"NAVALPLAN_DISABLE_TRACING":      "true",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !cfg.DisableTracing {
		t.Error("DisableTracing = false, want true")
	}
}

func TestNew_ProjectIDFallbacks(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{
			name: "prefers GOOGLE_CLOUD_PROJECT",
			vars: map[string]string{
				"GOOGLE_CLOUD_PROJECT": "primary-project",
				"GCP_PROJECT":          "secondary-project",
				"PROJECT_ID":           "tertiary-project",
			},
			want: "primary-project",
		},
		{
			name: "falls back to GCP_PROJECT",
			vars: map[string]string{
				"GCP_PROJECT":    "secondary-project",
				"GCLOUD_PROJECT": "tertiary-project",
			},
			want: "secondary-project",
		},
		{
			name: "falls back to GCLOUD_PROJECT",
			vars: map[string]string{
				"GCLOUD_PROJECT": "tertiary-project",
				"PROJECT_ID":     "quaternary-project",
			},
			want: "tertiary-project",
		},
		{
			name: "falls back to PROJECT_ID",
			vars: map[string]string{
				"PROJECT_ID": "quaternary-project",
			},
			want: "quaternary-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]string{
				"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
				"GEMINI_API_KEY":                 "gemini-key",
			}
			for k, v := range tt.vars {
				vars[k] = v
			}
			cfg, err := New(envMap(vars))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if cfg.Project != tt.want {
				t.Errorf("Project = %q, want %q", cfg.Project, tt.want)
			}
		})
	}
}

func TestNew_RAGCorpusConfig(t *testing.T) {
	tests := []struct {
		name         string
		vars         map[string]string
		wantCP       string
		wantNGA      string
		wantLocation string
	}{
		{
			name: "defaults",
			vars: map[string]string{
				"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
				"GEMINI_API_KEY":                 "gemini-key",
			},
			wantCP:       "coast-pilot-corpus",
			wantNGA:      "nga-sailing-directions-corpus",
			wantLocation: "us-central1",
		},
		{
			name: "explicit overrides",
			vars: map[string]string{
				"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
				"GEMINI_API_KEY":                 "gemini-key",
				"COAST_PILOT_CORPUS_ID":          "custom-cp-corpus",
				"NGA_CORPUS_ID":                  "custom-nga-corpus",
				"VERTEX_LOCATION":                "europe-west1",
			},
			wantCP:       "custom-cp-corpus",
			wantNGA:      "custom-nga-corpus",
			wantLocation: "europe-west1",
		},
		{
			name: "ignores global GOOGLE_CLOUD_LOCATION for VertexLocation",
			vars: map[string]string{
				"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
				"GEMINI_API_KEY":                 "gemini-key",
				"GOOGLE_CLOUD_LOCATION":          "global",
			},
			wantCP:       "coast-pilot-corpus",
			wantNGA:      "nga-sailing-directions-corpus",
			wantLocation: "us-central1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := New(envMap(tt.vars))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if cfg.CoastPilotCorpusID != tt.wantCP {
				t.Errorf("CoastPilotCorpusID = %q, want %q", cfg.CoastPilotCorpusID, tt.wantCP)
			}
			if cfg.NGACorpusID != tt.wantNGA {
				t.Errorf("NGACorpusID = %q, want %q", cfg.NGACorpusID, tt.wantNGA)
			}
			if cfg.VertexLocation != tt.wantLocation {
				t.Errorf("VertexLocation = %q, want %q", cfg.VertexLocation, tt.wantLocation)
			}
		})
	}
}

func TestModelForAgentAndSetUniformModel(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY":       "maps-key",
		"GEMINI_API_KEY":                       "gemini-key",
		"NAVALPLAN_AGENT_MODEL":                "gemini-3.5-flash-lite",
		"NAVALPLAN_HARBOURMASTER_MODEL":        "gemini-harbourmaster",
		"NAVALPLAN_PILOT_MODEL":                "gemini-pilot",
		"NAVALPLAN_DEBRIEF_MODEL":              "gemini-debrief",
		"NAVALPLAN_COMMODORE_MODEL":            "gemini-commodore",
		"NAVALPLAN_SPECIALIST_MODEL":           "gemini-specialist",
		"NAVALPLAN_LOOKOUT_MODEL":              "gemini-lookout",
		"NAVALPLAN_FACILITY_DISCOVERY_MODEL":   "gemini-facility",
		"NAVALPLAN_HYDROGRAPHIC_PILOT_MODEL":   "gemini-hydro",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	cases := map[string]string{
		"harbourmaster":                        "gemini-harbourmaster",
		"pilot":                                "gemini-pilot",
		"debrief":                              "gemini-debrief",
		"commodore":                            "gemini-commodore",
		"specialist":                           "gemini-specialist",
		"lookout":                              "gemini-lookout",
		"facility_discovery":                   "gemini-facility",
		"hydrographic_pilot":                   "gemini-hydro",
		"harbourmaster_search_specialist":      "gemini-3.5-flash-lite",
		"facility_discovery_search_specialist": "gemini-3.5-flash-lite",
		"unknown_agent":                        "gemini-3.5-flash-lite",
	}
	for agentName, want := range cases {
		if got := cfg.ModelForAgent(agentName); got != want {
			t.Errorf("ModelForAgent(%q) = %q, want %q", agentName, got, want)
		}
	}

	cfg.SetUniformModel("gemini-2.5-pro")
	for agentName := range cases {
		want := "gemini-2.5-pro"
		if agentName == "harbourmaster_search_specialist" || agentName == "facility_discovery_search_specialist" {
			want = "gemini-3.5-flash-lite"
		}
		if got := cfg.ModelForAgent(agentName); got != want {
			t.Errorf("after SetUniformModel, ModelForAgent(%q) = %q, want %q", agentName, got, want)
		}
	}

	cfg.SetTargetAgentModel("harbourmaster", "gemini-4-argon")
	if got := cfg.ModelForAgent("harbourmaster"); got != "gemini-4-argon" {
		t.Errorf("after SetTargetAgentModel(harbourmaster), ModelForAgent(harbourmaster) = %q, want gemini-4-argon", got)
	}
	if got := cfg.ModelForAgent("facility_discovery"); got != "gemini-3.5-flash-lite" {
		t.Errorf("after SetTargetAgentModel(harbourmaster), ModelForAgent(facility_discovery) = %q, want gemini-3.5-flash-lite", got)
	}
	if got := cfg.ModelForAgent("hydrographic_pilot"); got != "gemini-3.5-flash-lite" {
		t.Errorf("after SetTargetAgentModel(harbourmaster), ModelForAgent(hydrographic_pilot) = %q, want gemini-3.5-flash-lite", got)
	}
}

func TestIsClaudeModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"claude-sonnet-4-5", true},
		{"claude-haiku-4-5", true},
		{"claude-opus-4-5", true},
		{"claude-3-7-sonnet@20250219", true},
		{"CLAUDE-SONNET-4-5", true},
		{"gemini-3.5-flash-lite", false},
		{"gemini-4-argon", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsClaudeModel(tc.model); got != tc.want {
			t.Errorf("IsClaudeModel(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestClaudeSearchSpecialistFallback(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
		"GEMINI_API_KEY":                 "gemini-key",
		"NAVALPLAN_SEARCH_MODEL":         "gemini-2.5-flash",
		"NAVALPLAN_PILOT_MODEL":          "claude-sonnet-4-5",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfg.SearchModelName != "gemini-2.5-flash" {
		t.Errorf("SearchModelName = %q, want gemini-2.5-flash", cfg.SearchModelName)
	}
	if got := cfg.ModelForAgent("pilot"); got != "claude-sonnet-4-5" {
		t.Errorf("ModelForAgent(pilot) = %q, want claude-sonnet-4-5", got)
	}
	if got := cfg.ModelForAgent("pilot_search_specialist"); got != "gemini-2.5-flash" {
		t.Errorf("ModelForAgent(pilot_search_specialist) = %q, want gemini-2.5-flash", got)
	}

	// Uniform Claude model should keep search specialists on Gemini
	cfg.SetUniformModel("claude-haiku-4-5")
	if got := cfg.ModelForAgent("harbourmaster"); got != "claude-haiku-4-5" {
		t.Errorf("ModelForAgent(harbourmaster) = %q, want claude-haiku-4-5", got)
	}
	if got := cfg.ModelForAgent("harbourmaster_search_specialist"); got != "gemini-2.5-flash" {
		t.Errorf("ModelForAgent(harbourmaster_search_specialist) = %q, want gemini-2.5-flash", got)
	}

	// Verify default SearchModelName
	cfgFallback, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY": "maps-key",
		"GEMINI_API_KEY":                 "gemini-key",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfgFallback.SearchModelName != "gemini-3.5-flash-lite" {
		t.Errorf("SearchModelName = %q, want gemini-3.5-flash-lite", cfgFallback.SearchModelName)
	}
}
