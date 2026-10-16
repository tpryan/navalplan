// Package config loads and validates runtime configuration for the researcher service.
package config

import (
	"fmt"
	"strconv"

	"cloud.google.com/go/compute/metadata"
)

type Config struct {
	Env                string
	Project            string
	ModelName          string
	CommodoreModelName string
	// UseVertexAI controls how Gemini model calls authenticate:
	// - true (GOOGLE_GENAI_USE_VERTEXAI="true" or "1"): Calls the Vertex AI Gemini endpoint
	//   using Google Cloud Application Default Credentials (ADC) — i.e., the attached GCP
	//   Service Account IAM identity in Cloud Run / Vertex AI Agent Runtime, or local
	//   `gcloud auth application-default login` credentials. No GEMINI_API_KEY is needed.
	// - false (default): Calls the Google AI Studio Gemini endpoint using GeminiAPIKey.
	UseVertexAI        bool
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
	VertexLocation     string
	DisableTracing     bool
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
		Env:                env,
		Project:            project,
		ModelName:          modelName,
		CommodoreModelName: commodoreModelName,
		UseVertexAI:        useVertexAI,
		GeminiLocation:     geminiLocation,
		GeminiAPIKey:       geminiKey,
		MapsAPIKey:         mapsKey,
		UKTidalAPIKey:      ukTidalKey,
		NIWAAPIKey:         niwaKey,
		Port:               port,
		BaseURL:            baseURL,
		ThinkingBudget:     thinkingBudget,
		SearchTimeoutMs:    searchTimeoutMs,
		CoastPilotCorpusID: coastPilotCorpus,
		NGACorpusID:        ngaCorpus,
		VertexLocation:     vertexLocation,
		DisableTracing:     getEnv("NAVALPLAN_DISABLE_TRACING") == "true",
	}

	return cfg, nil
}
