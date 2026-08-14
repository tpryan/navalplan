package server

import (
	"app/internal/server/handlers"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type CloudLoggingHandler struct {
	Handler       slog.Handler
	FormatMessage bool
}

func (h *CloudLoggingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h *CloudLoggingHandler) Handle(ctx context.Context, r slog.Record) error {
	var styledDur string
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "duration" {
			durStr := fmt.Sprintf("%v", a.Value.Any())
			dur, err := time.ParseDuration(durStr)
			if err == nil {
				var color string
				switch {
				case dur < time.Millisecond:
					color = "255" // White
				case dur < time.Second:
					color = "226" // Yellow
				case dur < time.Minute:
					color = "208" // Orange
				default:
					color = "196" // Red
				}
				styledDur = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(durStr)
				return true
			}
		}
		newRecord.AddAttrs(a)
		return true
	})

	var sb strings.Builder
	sb.WriteString(newRecord.Message)

	if styledDur != "" {
		sb.WriteString(" ")
		keyStyle := lipgloss.NewStyle().Bold(true)
		sb.WriteString(keyStyle.Render("duration"))
		sb.WriteString("=")
		sb.WriteString(styledDur)
	}

	if h.FormatMessage {
		newRecord.Attrs(func(a slog.Attr) bool {
			sb.WriteString(" ")
			sb.WriteString(a.Key)
			sb.WriteString("=")
			sb.WriteString(fmt.Sprintf("%v", a.Value.Any()))
			return true
		})
	}
	newRecord.Message = sb.String()

	if trace := handlers.GetTraceFromContext(ctx); trace != "" {
		newRecord.Add("logging.googleapis.com/trace", slog.StringValue(trace))
	}
	return h.Handler.Handle(ctx, newRecord)
}

func (h *CloudLoggingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CloudLoggingHandler{Handler: h.Handler.WithAttrs(attrs), FormatMessage: h.FormatMessage}
}

func (h *CloudLoggingHandler) WithGroup(name string) slog.Handler {
	return &CloudLoggingHandler{Handler: h.Handler.WithGroup(name), FormatMessage: h.FormatMessage}
}
