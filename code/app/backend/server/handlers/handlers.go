package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"app/datastore"
	"app/models"
	"app/service"

	"google.golang.org/api/idtoken"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	DB          datastore.Store
	ContentDir  string
	AgentURL    string
	AgentClient *http.Client
	Agent       *service.AgentRunner
	ResearchSem chan struct{}

	// recStreams holds active SSE channels keyed by session ID.
	// Moved from package-level globals to enable per-instance isolation and testing.
	muRecStreams sync.RWMutex
	recStreams   map[string]chan models.VoyageRecommendation

	// progressStreams holds active progress SSE channels keyed by session ID.
	muProgressStreams sync.RWMutex
	progressStreams   map[string]chan models.ProgressEvent

	// activeJobs tracks in-flight agent research tasks keyed by job key (e.g. "guide:42", "stop:7", "full:42").
	muActiveJobs sync.Mutex
	activeJobs   map[string]struct{}
}

// New creates a new Handler with the given dependencies.
func New(db datastore.Store, contentDir string, agentURL string) *Handler {
	var client *http.Client
	var err error

	if agentURL != "" && strings.Contains(agentURL, "run.app") {
		// Create an authenticated client for Cloud Run
		client, err = idtoken.NewClient(context.Background(), agentURL)
		if err != nil {
			slog.Error("Failed to create authenticated agent client", "error", err)
			client = &http.Client{Timeout: 20 * time.Minute}
		} else {
			client.Timeout = 20 * time.Minute
		}
	} else {
		client = &http.Client{Timeout: 20 * time.Minute}
	}

	return &Handler{
		DB:          db,
		ContentDir:  contentDir,
		AgentURL:    agentURL,
		AgentClient: client,
		Agent:       &service.AgentRunner{Client: client, BaseURL: agentURL},
		ResearchSem: make(chan struct{}, 10),
		recStreams:       make(map[string]chan models.VoyageRecommendation),
		progressStreams:   make(map[string]chan models.ProgressEvent),
		activeJobs:        make(map[string]struct{}),
	}
}

// tryClaimJob atomically marks a job key as in-flight. Returns false if already running.
func (h *Handler) tryClaimJob(key string) bool {
	h.muActiveJobs.Lock()
	defer h.muActiveJobs.Unlock()
	if _, ok := h.activeJobs[key]; ok {
		return false
	}
	h.activeJobs[key] = struct{}{}
	return true
}

// releaseJob removes a job key from the active set.
func (h *Handler) releaseJob(key string) {
	h.muActiveJobs.Lock()
	defer h.muActiveJobs.Unlock()
	delete(h.activeJobs, key)
}

// CheckAgentHealth pings the agent's /healthz endpoint.
func (h *Handler) CheckAgentHealth(ctx context.Context) error {
	if h.AgentURL == "" {
		return nil // Agent not configured, skip check
	}

	url := h.AgentURL + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := h.AgentClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent returned non-200 status: %d", resp.StatusCode)
	}

	return nil
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)

	// Find the first valid start of a JSON ( { or [ )
	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")

	start := -1
	if startObj != -1 && (startArr == -1 || startObj < startArr) {
		start = startObj
	} else {
		start = startArr
	}

	// Find the last valid end of a JSON ( } or ] )
	endObj := strings.LastIndex(s, "}")
	endArr := strings.LastIndex(s, "]")

	end := -1
	if endObj != -1 && (endArr == -1 || endObj > endArr) {
		end = endObj
	} else {
		end = endArr
	}

	if start != -1 && end != -1 && end > start {
		s = s[start : end+1]
	} else {
		// No JSON found
		return ""
	}

	// Fix common LLM JSON error: unescaped single quotes or unnecessary escapes
	s = strings.ReplaceAll(s, `\'`, `'`)
	return strings.TrimSpace(s)
}

// writeError writes a consistent JSON error response: {"error": "<message>"}.
// It replaces ad-hoc http.Error calls so all API error bodies share one format.
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
