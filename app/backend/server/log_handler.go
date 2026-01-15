package server

import (
	"context"
	"log/slog"

	appcontext "app/context"
)

type CloudLoggingHandler struct {
	Handler slog.Handler
}

func (h *CloudLoggingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h *CloudLoggingHandler) Handle(ctx context.Context, r slog.Record) error {
	if trace := appcontext.GetTraceFromContext(ctx); trace != "" {
		r.Add("logging.googleapis.com/trace", slog.StringValue(trace))
	}
	return h.Handler.Handle(ctx, r)
}

func (h *CloudLoggingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CloudLoggingHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *CloudLoggingHandler) WithGroup(name string) slog.Handler {
	return &CloudLoggingHandler{Handler: h.Handler.WithGroup(name)}
}
