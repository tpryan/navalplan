package handlers

import (
	"context"

	"app/internal/model"

	"go.opentelemetry.io/otel/trace"
)

type contextKey int

const (
	personKey contextKey = iota
	traceKey
)

// AddPersonToContext adds the person to the context
func AddPersonToContext(ctx context.Context, person *model.Person) context.Context {
	return context.WithValue(ctx, personKey, person)
}

// GetPersonFromContext returns the person from the context
func GetPersonFromContext(ctx context.Context) *model.Person {
	person, ok := ctx.Value(personKey).(*model.Person)
	if !ok {
		return nil
	}
	return person
}

// AddTraceToContext adds the trace to the context
func AddTraceToContext(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey, traceID)
}

// GetTraceFromContext returns the trace from the context.
// It first checks for an OpenTelemetry span, then falls back to the manual context value.
func GetTraceFromContext(ctx context.Context) string {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		return span.TraceID().String()
	}

	traceVal, ok := ctx.Value(traceKey).(string)
	if !ok {
		return ""
	}
	return traceVal
}
