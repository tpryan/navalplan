package handlers

import (
	"context"
	"fmt"

	"google.golang.org/api/docs/v1"
	"google.golang.org/api/option"
)

type GoogleDocsService struct {
}

func NewGoogleDocsService() *GoogleDocsService {
	return &GoogleDocsService{}
}

func (s *GoogleDocsService) getService(ctx context.Context) (*docs.Service, error) {
	// Uses Application Default Credentials
	// In a real app, you might cache the service or client
	return docs.NewService(ctx, option.WithScopes(docs.DocumentsScope, docs.DriveScope))
}

func (s *GoogleDocsService) Create(ctx context.Context, title string) (*docs.Document, error) {
	srv, err := s.getService(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to init docs service: %w", err)
	}

	doc := &docs.Document{
		Title: title,
	}
	return srv.Documents.Create(doc).Do()
}

func (s *GoogleDocsService) BatchUpdate(ctx context.Context, docID string, requests []*docs.Request) error {
	srv, err := s.getService(ctx)
	if err != nil {
		return fmt.Errorf("failed to init docs service: %w", err)
	}

	_, err = srv.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: requests,
	}).Do()
	return err
}
