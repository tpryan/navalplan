package server_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"app/config"
	"app/server"

	"github.com/stretchr/testify/assert"
)

// RecordingHandler is a slog.Handler that records the last record it handled.
type RecordingHandler struct {
	mu         sync.Mutex
	LastRecord *slog.Record
	LastAttrs  map[string]any
}

func (h *RecordingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *RecordingHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.LastRecord = &r
	h.LastAttrs = make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		h.LastAttrs[a.Key] = a.Value.Any()
		return true
	})
	return nil
}

func (h *RecordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *RecordingHandler) WithGroup(name string) slog.Handler {
	return h
}

func TestTraceMiddlewareLogging(t *testing.T) {
	// Setup custom logger
	recorder := &RecordingHandler{}
	// Wrap with CloudLoggingHandler to ensure it adds the trace from context to the record
	cloudHandler := &server.CloudLoggingHandler{Handler: recorder}
	logger := slog.New(cloudHandler)
	slog.SetDefault(logger)

	// Setup Server
	mockStore := new(MockStore)
	cfg := &config.Config{
		ContentDir: ".",
		Project:    "test-project",
	}
	srv, err := server.New(mockStore, cfg)
	assert.NoError(t, err)

	// Simple handler
	srv.Mux.HandleFunc("/test-trace", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Create Request with Trace Header
	req := httptest.NewRequest("GET", "/test-trace", nil)
	traceID := "1234567890abcdef"
	req.Header.Set("X-Cloud-Trace-Context", traceID+"/0;o=1")

	// Run through Middleware
	w := httptest.NewRecorder()
	handler := srv.Middleware(srv.Mux)
	handler.ServeHTTP(w, req)

	// Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	// Check if log was recorded
	assert.NotNil(t, recorder.LastRecord, "Log record should have been created")

	// Check if trace attribute is present in the log record
	// The CloudLoggingHandler adds "logging.googleapis.com/trace"
	// We need to check if the recorder captured it.
	// Since CloudLoggingHandler.Handle modifies the record before passing to Inner,
	// checking recorder.LastRecord or LastAttrs should work.

	// However, slog.Record.Attrs() iterates over attributes.
	// CloudLoggingHandler uses r.Add() which adds to the record's internal list.

	var foundTrace string
	recorder.LastRecord.Attrs(func(a slog.Attr) bool {
		if a.Key == "logging.googleapis.com/trace" {
			foundTrace = a.Value.String()
			return false // stop
		}
		return true
	})

	expectedTrace := "projects/test-project/traces/" + traceID
	assert.Equal(t, expectedTrace, foundTrace, "Trace ID should be present in log attributes")
}
