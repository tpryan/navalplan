package logging

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

type key int

const (
	traceKey key = iota
	spanKey
)

// AddTraceToContext adds the trace to the context
func AddTraceToContext(ctx context.Context, trace string) context.Context {
	return context.WithValue(ctx, traceKey, trace)
}

// GetTraceFromContext returns the trace from the context.
// It first checks for an OpenTelemetry span, then falls back to the manual context value.
func GetTraceFromContext(ctx context.Context) string {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		return span.TraceID().String()
	}

	trace, ok := ctx.Value(traceKey).(string)
	if !ok {
		return ""
	}
	return trace
}

// AddSpanToContext adds the span ID to the context
func AddSpanToContext(ctx context.Context, span string) context.Context {
	return context.WithValue(ctx, spanKey, span)
}

// GetSpanFromContext returns the span ID from the context.
// It first checks for an OpenTelemetry span, then falls back to the manual context value.
func GetSpanFromContext(ctx context.Context) string {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		return span.SpanID().String()
	}

	span, ok := ctx.Value(spanKey).(string)
	if !ok {
		return ""
	}
	return span
}
