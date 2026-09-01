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

	geminiKey := getEnv("GEMINI_API_KEY")
	if geminiKey == "" {
		geminiKey = getEnv("GOOGLE_API_KEY")
	}
	if geminiKey == "" {
		geminiKey = getEnv("NAVALPLAN_GEMINI_KEY")
	}
	if geminiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
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
		vertexLocation = getEnv("GOOGLE_CLOUD_LOCATION")
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
