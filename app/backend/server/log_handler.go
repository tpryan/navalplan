package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appcontext "app/context"

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
	// Filter out the duration attribute if it exists, and style it.
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

	// Format the message with optional styled duration.
	var sb strings.Builder
	sb.WriteString(newRecord.Message)

	if styledDur != "" {
		sb.WriteString(" ")
		// Use a bold key for "duration" to match charm style
		keyStyle := lipgloss.NewStyle().Bold(true)
		sb.WriteString(keyStyle.Render("duration"))
		sb.WriteString("=")
		sb.WriteString(styledDur)
	}

	// Format Message if enabled (legacy behavior)
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

	if trace := appcontext.GetTraceFromContext(ctx); trace != "" {
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
