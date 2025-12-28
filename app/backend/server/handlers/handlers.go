package handlers

import (
	"context"

	"app/datastore"

	"google.golang.org/api/docs/v1"
)

type DocsService interface {
	Create(ctx context.Context, title string) (*docs.Document, error)
	BatchUpdate(ctx context.Context, docID string, requests []*docs.Request) error
}

type Handler struct {
	DB   datastore.Store
	Docs DocsService
}

func New(db datastore.Store, docsService DocsService) *Handler {
	return &Handler{
		DB:   db,
		Docs: docsService,
	}
}
