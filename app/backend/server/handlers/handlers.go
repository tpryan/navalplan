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

	// Find the first '{' and the last '}'
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")

	if start != -1 && end != -1 && end > start {
		s = s[start : end+1]
	} else {
		// No JSON object found
		return ""
	}

	// Fix common LLM JSON error: unescaped single quotes or unnecessary escapes
	s = strings.ReplaceAll(s, `\'`, `'`)
	return strings.TrimSpace(s)
}
