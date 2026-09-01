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
		t.Fatal("New() should error when no Gemini API key is set")
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
		t.Errorf("Port = %q, want 3000 (PORT should win over NAVALPLAN_AGENT_PORT)", cfg.Port)
	}
}

func TestNew_ThinkingBudgetOverride(t *testing.T) {
	cfg, err := New(envMap(map[string]string{
		"NAVALPLAN_BACKEND_MAPS_API_KEY":  "maps-key",
		"GEMINI_API_KEY":                  "gemini-key",
		"NAVALPLAN_AGENT_THINKING_BUDGET": "-1",
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cfg.ThinkingBudget != -1 {
		t.Errorf("ThinkingBudget = %d, want -1", cfg.ThinkingBudget)
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
				"GCP_PROJECT": "secondary-project",
				"PROJECT_ID":  "tertiary-project",
			},
			want: "secondary-project",
		},
		{
			name: "falls back to GCLOUD_PROJECT",
			vars: map[string]string{
				"GCLOUD_PROJECT": "gcloud-project",
				"PROJECT_ID":     "tertiary-project",
			},
			want: "gcloud-project",
		},
		{
			name: "falls back to PROJECT_ID",
			vars: map[string]string{
				"PROJECT_ID": "tertiary-project",
			},
			want: "tertiary-project",
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
