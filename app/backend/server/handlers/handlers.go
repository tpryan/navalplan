package handlers

import (
	"context"

	"app/datastore"

	"google.golang.org/api/docs/v1"
)

// DocsService defines the interface for interacting with Google Docs.
type DocsService interface {
	Create(ctx context.Context, title string) (*docs.Document, error)
	BatchUpdate(ctx context.Context, docID string, requests []*docs.Request) error
}

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	DB         datastore.Store
	Docs       DocsService
	ContentDir string
	AgentURL   string
}

// New creates a new Handler with the given dependencies.
func New(db datastore.Store, docsService DocsService, contentDir string, agentURL string) *Handler {
	return &Handler{
		DB:         db,
		Docs:       docsService,
		ContentDir: contentDir,
		AgentURL:   agentURL,
	}
}
