package handlers

import (
	"context"
	"net/http"
	"strings"

	"app/datastore"

	"github.com/charmbracelet/log"
	"google.golang.org/api/docs/v1"
	"google.golang.org/api/idtoken"
)

// DocsService defines the interface for interacting with Google Docs.
type DocsService interface {
	Create(ctx context.Context, title string) (*docs.Document, error)
	BatchUpdate(ctx context.Context, docID string, requests []*docs.Request) error
}

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	DB          datastore.Store
	Docs        DocsService
	ContentDir  string
	AgentURL    string
	AgentClient *http.Client
	ResearchSem chan struct{}
}

// New creates a new Handler with the given dependencies.
func New(db datastore.Store, docsService DocsService, contentDir string, agentURL string) *Handler {
	var client *http.Client
	var err error

	if agentURL != "" && strings.Contains(agentURL, "run.app") {
		// Create an authenticated client for Cloud Run
		client, err = idtoken.NewClient(context.Background(), agentURL)
		if err != nil {
			log.Errorf("Failed to create authenticated agent client: %v", err)
			client = http.DefaultClient
		}
	} else {
		client = http.DefaultClient
	}

	return &Handler{
		DB:          db,
		Docs:        docsService,
		ContentDir:  contentDir,
		AgentURL:    agentURL,
		AgentClient: client,
		ResearchSem: make(chan struct{}, 10), // Limit to 10 concurrent research tasks globally
	}
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
