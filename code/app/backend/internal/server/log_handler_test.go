package server_test

import (
	"context"
	"log/slog"
	"testing"

	"app/internal/server"
	"app/internal/server/handlers"

	"github.com/stretchr/testify/assert"
)

type CaptureHandler struct {
	CapturedRecord slog.Record
}

func (h *CaptureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *CaptureHandler) Handle(ctx context.Context, r slog.Record) error {
	h.CapturedRecord = r
	return nil
}

func (h *CaptureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *CaptureHandler) WithGroup(name string) slog.Handler {
	return h
}

func TestCloudLoggingHandler_FormatMessage(t *testing.T) {
	capture := &CaptureHandler{}
	h := &server.CloudLoggingHandler{
		Handler:       capture,
		FormatMessage: true,
	}
	logger := slog.New(h)

	ctx := context.Background()
	logger.InfoContext(ctx, "Test Message", "key1", "value1", "key2", 123)

	assert.Equal(t, "Test Message key1=value1 key2=123", capture.CapturedRecord.Message)

	var foundKey1 bool
	capture.CapturedRecord.Attrs(func(a slog.Attr) bool {
		if a.Key == "key1" {
			foundKey1 = true
			return false
		}
		return true
	})
	assert.True(t, foundKey1, "Original attributes should be preserved in the record")
}

func TestCloudLoggingHandler_TraceInjection(t *testing.T) {
	capture := &CaptureHandler{}
	h := &server.CloudLoggingHandler{
		Handler:       capture,
		FormatMessage: false,
	}
	logger := slog.New(h)

	traceID := "test-trace-id"
	ctx := handlers.AddTraceToContext(context.Background(), traceID)

	logger.InfoContext(ctx, "Trace Test")

	var traceValue string
	capture.CapturedRecord.Attrs(func(a slog.Attr) bool {
		if a.Key == "logging.googleapis.com/trace" {
			traceValue = a.Value.String()
			return false
		}
		return true
	})
	assert.Equal(t, traceID, traceValue)
}
