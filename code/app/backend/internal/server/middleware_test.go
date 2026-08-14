package server_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"app/internal/config"
	"app/internal/server"

	"github.com/stretchr/testify/assert"
)

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
	recorder := &RecordingHandler{}
	cloudHandler := &server.CloudLoggingHandler{Handler: recorder}
	logger := slog.New(cloudHandler)
	slog.SetDefault(logger)

	mockStore := new(MockStore)
	cfg := &config.Config{
		ContentDir: ".",
		Project:    "test-project",
	}
	srv, err := server.New(mockStore, cfg)
	assert.NoError(t, err)

	srv.Mux.HandleFunc("/test-trace", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test-trace", nil)
	traceID := "1234567890abcdef"
	req.Header.Set("X-Cloud-Trace-Context", traceID+"/0;o=1")

	w := httptest.NewRecorder()
	handler := srv.Middleware(srv.Mux)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, recorder.LastRecord, "Log record should have been created")

	var foundTrace string
	recorder.LastRecord.Attrs(func(a slog.Attr) bool {
		if a.Key == "logging.googleapis.com/trace" {
			foundTrace = a.Value.String()
			return false
		}
		return true
	})

	expectedTrace := "projects/test-project/traces/" + traceID
	assert.Equal(t, expectedTrace, foundTrace, "Trace ID should be present in log attributes")
}

func TestSanitizePathMiddleware(t *testing.T) {
	tests := []struct {
		name         string
		requestURL   string
		expectedPath string
	}{
		{
			name:         "Clean path unchanged",
			requestURL:   "/api/admin/maintenance",
			expectedPath: "/api/admin/maintenance",
		},
		{
			name:         "Trailing space in URL sanitized",
			requestURL:   "/api/admin/maintenance%20",
			expectedPath: "/api/admin/maintenance",
		},
		{
			name:         "Trailing slash in API path sanitized",
			requestURL:   "/api/admin/maintenance/",
			expectedPath: "/api/admin/maintenance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := new(MockStore)
			cfg := &config.Config{ContentDir: ".", Project: "test"}
			srv, err := server.New(mockStore, cfg)
			assert.NoError(t, err)

			var capturedPath string
			srv.Mux.HandleFunc("POST /api/admin/maintenance", func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("POST", tt.requestURL, nil)
			rec := httptest.NewRecorder()
			handler := srv.Middleware(srv.Mux)
			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tt.expectedPath, capturedPath)
		})
	}
}
