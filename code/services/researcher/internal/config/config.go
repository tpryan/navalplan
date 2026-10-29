// Package config loads and validates runtime configuration for the researcher service.
package config

import (
	"fmt"
	"strconv"
	"strings"

	"cloud.google.com/go/compute/metadata"
)

type Config struct {
	Env                        string
	Project                    string
	ModelName                  string
	HarbourmasterModelName     string
	PilotModelName             string
	DebriefModelName           string
	CommodoreModelName         string
	SpecialistModelName        string
	LookoutModelName           string
	FacilityDiscoveryModelName string
	HydrographicPilotModelName string
	SearchModelName            string
	// UseVertexAI controls how Gemini model calls authenticate:
	// - true (GOOGLE_GENAI_USE_VERTEXAI="true" or "1"): Calls the Vertex AI Gemini endpoint
	//   using Google Cloud Application Default Credentials (ADC) — i.e., the attached GCP
	//   Service Account IAM identity in Cloud Run / Vertex AI Agent Runtime, or local
	//   `gcloud auth application-default login` credentials. No GEMINI_API_KEY is needed.
	// - false (default): Calls the Google AI Studio Gemini endpoint using GeminiAPIKey.
	UseVertexAI bool
	// GeminiLocation is the Vertex AI location used for Gemini model calls when UseVertexAI
	// is true (parsed from GOOGLE_CLOUD_LOCATION; defaults to "global" because preview/lite
	// Gemini models are served on the global Vertex AI endpoint).
	GeminiLocation     string
	GeminiAPIKey       string
	MapsAPIKey         string
	UKTidalAPIKey      string
	NIWAAPIKey         string
	Port               string
	BaseURL            string
	ThinkingBudget     int32
	SearchTimeoutMs    int
	CoastPilotCorpusID string
	NGACorpusID        string
	// VertexLocation is the regional Vertex AI location used for RAG Engine corpora
	// (Coast Pilot and NGA Sailing Directions), which must be a concrete GCP region
	// such as "us-central1" rather than "global".
	VertexLocation string
	DisableTracing bool
}

// IsClaudeModel reports whether the given model identifier refers to an
// Anthropic Claude model (e.g. "claude-sonnet-4-5", "claude-3-7-sonnet@20250219").
func IsClaudeModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "claude")
}

// ModelForAgent returns the model configured for a specific agent name,
// falling back to c.ModelName (or "gemini-3.5-flash-lite" if unset).
// For "*_search_specialist" sub-agents, ModelForAgent always returns a fast
// Gemini model (c.SearchModelName or "gemini-3.5-flash-lite") because
// batch_google_search fires up to 10 concurrent GoogleSearch grounding calls
// with a 30s timeout and fails with 504 DEADLINE_EXCEEDED on heavy reasoning
// models (like gemini-4-argon or gemini-3.8-flash) or non-Gemini models.
func (c *Config) ModelForAgent(name string) string {
	if c == nil {
		return "gemini-3.5-flash-lite"
	}
	if strings.HasSuffix(name, "_search_specialist") {
		if c.SearchModelName != "" && !IsClaudeModel(c.SearchModelName) {
			return c.SearchModelName
		}
		return "gemini-3.5-flash-lite"
	}
	var specific string
	switch name {
	case "harbourmaster":
		specific = c.HarbourmasterModelName
	case "pilot":
		specific = c.PilotModelName
	case "debrief":
		specific = c.DebriefModelName
	case "commodore":
		specific = c.CommodoreModelName
	case "specialist":
		specific = c.SpecialistModelName
	case "lookout":
		specific = c.LookoutModelName
	case "facility_discovery":
		specific = c.FacilityDiscoveryModelName
	case "hydrographic_pilot":
		specific = c.HydrographicPilotModelName
	}
	if specific != "" {
		return specific
	}
	if c.ModelName != "" {
		return c.ModelName
	}
	return "gemini-3.5-flash-lite"
}

// SetUniformModel sets ModelName and all per-agent model fields to the given
// model.
func (c *Config) SetUniformModel(model string) {
	if c == nil {
		return
	}
	c.ModelName = model
	c.HarbourmasterModelName = model
	c.PilotModelName = model
	c.DebriefModelName = model
	c.CommodoreModelName = model
	c.SpecialistModelName = model
	c.LookoutModelName = model
	c.FacilityDiscoveryModelName = model
	c.HydrographicPilotModelName = model
}

// SetTargetAgentModel configures only targetAgent to run on candidateModel while
// keeping helper sub-agents (facility_discovery, hydrographic_pilot, and
// *_search_specialist) on the fast baseline model ("gemini-3.5-flash-lite").
// This isolates the model under test and prevents 3x nested slow-model timeouts
// when evaluating parent orchestrator agents (harbourmaster, specialist, lookout).
func (c *Config) SetTargetAgentModel(targetAgent, candidateModel string) {
	if c == nil {
		return
	}
	baseline := "gemini-3.5-flash-lite"
	c.ModelName = baseline
	c.HarbourmasterModelName = baseline
	c.PilotModelName = baseline
	c.DebriefModelName = baseline
	c.CommodoreModelName = baseline
	c.SpecialistModelName = baseline
	c.LookoutModelName = baseline
	c.FacilityDiscoveryModelName = baseline
	c.HydrographicPilotModelName = baseline

	switch targetAgent {
	case "harbourmaster":
		c.HarbourmasterModelName = candidateModel
	case "pilot":
		c.PilotModelName = candidateModel
	case "debrief":
		c.DebriefModelName = candidateModel
	case "commodore":
		c.CommodoreModelName = candidateModel
	case "specialist":
		c.SpecialistModelName = candidateModel
	case "lookout":
		c.LookoutModelName = candidateModel
	case "facility_discovery":
		c.FacilityDiscoveryModelName = candidateModel
	case "hydrographic_pilot":
		c.HydrographicPilotModelName = candidateModel
	default:
		c.ModelName = candidateModel
	}
}

func New(getEnv func(string) string) (*Config, error) {
	mapsKey := getEnv("NAVALPLAN_BACKEND_MAPS_API_KEY")
	if mapsKey == "" {
		mapsKey = getEnv("GOOGLE_MAPS_API_KEY")
	}
	if mapsKey == "" {
		return nil, fmt.Errorf("NAVALPLAN_BACKEND_MAPS_API_KEY is not set")
	}

	modelName := getEnv("NAVALPLAN_AGENT_MODEL")
	if modelName == "" {
		modelName = "gemini-3.5-flash-lite"
	}

	commodoreModelName := getEnv("NAVALPLAN_COMMODORE_MODEL")
	if commodoreModelName == "" {
		commodoreModelName = "gemini-4-argon"
	}

	harbourmasterModelName := getEnv("NAVALPLAN_HARBOURMASTER_MODEL")
	pilotModelName := getEnv("NAVALPLAN_PILOT_MODEL")
	debriefModelName := getEnv("NAVALPLAN_DEBRIEF_MODEL")
	specialistModelName := getEnv("NAVALPLAN_SPECIALIST_MODEL")
	lookoutModelName := getEnv("NAVALPLAN_LOOKOUT_MODEL")
	facilityDiscoveryModelName := getEnv("NAVALPLAN_FACILITY_DISCOVERY_MODEL")
	hydrographicPilotModelName := getEnv("NAVALPLAN_HYDROGRAPHIC_PILOT_MODEL")
	searchModelName := getEnv("NAVALPLAN_SEARCH_MODEL")
	if searchModelName == "" {
		searchModelName = "gemini-3.5-flash-lite"
	}

	// Determine whether Gemini calls should authenticate via Vertex AI IAM (ADC)
	// or via a static Google AI Studio API key (GEMINI_API_KEY).
	// When GOOGLE_GENAI_USE_VERTEXAI is enabled ("true" or "1"), the genai SDK
	// uses the runtime's GCP service account credentials and ignores GeminiAPIKey,
	// so GEMINI_API_KEY is only required when UseVertexAI is false.
	useVertexEnv := getEnv("GOOGLE_GENAI_USE_VERTEXAI")
	useVertexAI := useVertexEnv == "true" || useVertexEnv == "1"

	geminiLocation := getEnv("GOOGLE_CLOUD_LOCATION")
	if geminiLocation == "" {
		geminiLocation = "global"
	}

	geminiKey := getEnv("GEMINI_API_KEY")
	if geminiKey == "" {
		geminiKey = getEnv("GOOGLE_API_KEY")
	}
	if geminiKey == "" {
		geminiKey = getEnv("NAVALPLAN_GEMINI_KEY")
	}
	if geminiKey == "" && !useVertexAI {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set (required unless GOOGLE_GENAI_USE_VERTEXAI=true)")
	}

	port := getEnv("PORT")
	if port == "" {
		port = getEnv("NAVALPLAN_AGENT_PORT")
	}
	if port == "" {
		port = "8081"
	}

	baseURL := getEnv("NAVALPLAN_AGENT_BASE_URL")

	ukTidalKey := getEnv("NAVALPLAN_TIDAL_UKTIDAL_API_KEY")
	niwaKey := getEnv("NAVALPLAN_TIDAL_NIWA_API_KEY")

	env := getEnv("ENV")
	if env == "" {
		env = "development"
	}

	project := getEnv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		project = getEnv("GCP_PROJECT")
	}
	if project == "" {
		project = getEnv("GCLOUD_PROJECT")
	}
	if project == "" {
		project = getEnv("PROJECT_ID")
	}
	if project == "" && metadata.OnGCE() {
		if pid, err := metadata.ProjectID(); err == nil {
			project = pid
		}
	}

	thinkingBudget := int32(1024)
	if v := getEnv("NAVALPLAN_AGENT_THINKING_BUDGET"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			thinkingBudget = int32(n)
		}
	}

	searchTimeoutMs := 30000
	if v := getEnv("NAVALPLAN_AGENT_SEARCH_TIMEOUT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			searchTimeoutMs = n
		}
	}

	coastPilotCorpus := getEnv("COAST_PILOT_CORPUS_ID")
	if coastPilotCorpus == "" {
		coastPilotCorpus = getEnv("NAVALPLAN_COAST_PILOT_CORPUS_ID")
	}
	if coastPilotCorpus == "" {
		coastPilotCorpus = "coast-pilot-corpus"
	}

	ngaCorpus := getEnv("NGA_CORPUS_ID")
	if ngaCorpus == "" {
		ngaCorpus = getEnv("NAVALPLAN_NGA_CORPUS_ID")
	}
	if ngaCorpus == "" {
		ngaCorpus = "nga-sailing-directions-corpus"
	}

	vertexLocation := getEnv("VERTEX_LOCATION")
	if vertexLocation == "" {
		if loc := getEnv("GOOGLE_CLOUD_LOCATION"); loc != "global" {
			vertexLocation = loc
		}
	}
	if vertexLocation == "" {
		vertexLocation = getEnv("REGION")
	}
	if vertexLocation == "" {
		vertexLocation = "us-central1"
	}

	cfg := &Config{
		Env:                        env,
		Project:                    project,
		ModelName:                  modelName,
		HarbourmasterModelName:     harbourmasterModelName,
		PilotModelName:             pilotModelName,
		DebriefModelName:           debriefModelName,
		CommodoreModelName:         commodoreModelName,
		SpecialistModelName:        specialistModelName,
		LookoutModelName:           lookoutModelName,
		FacilityDiscoveryModelName: facilityDiscoveryModelName,
		HydrographicPilotModelName: hydrographicPilotModelName,
		SearchModelName:            searchModelName,
		UseVertexAI:                useVertexAI,
		GeminiLocation:             geminiLocation,
		GeminiAPIKey:               geminiKey,
		MapsAPIKey:                 mapsKey,
		UKTidalAPIKey:              ukTidalKey,
		NIWAAPIKey:                 niwaKey,
		Port:                       port,
		BaseURL:                    baseURL,
		ThinkingBudget:             thinkingBudget,
		SearchTimeoutMs:            searchTimeoutMs,
		CoastPilotCorpusID:         coastPilotCorpus,
		NGACorpusID:                ngaCorpus,
		VertexLocation:             vertexLocation,
		DisableTracing:             getEnv("NAVALPLAN_DISABLE_TRACING") == "true",
	}

	return cfg, nil
}
