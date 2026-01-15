package context

import (
	"context"

	"app/models"
)

type key int

const (
	personKey key = iota
	traceKey
)

// AddPersonToContext adds the person to the context
func AddPersonToContext(ctx context.Context, person *models.Person) context.Context {
	return context.WithValue(ctx, personKey, person)
}

// GetPersonFromContext returns the person from the context
func GetPersonFromContext(ctx context.Context) *models.Person {
	person, ok := ctx.Value(personKey).(*models.Person)
	if !ok {
		return nil
	}
	return person
}

// AddTraceToContext adds the trace to the context
func AddTraceToContext(ctx context.Context, trace string) context.Context {
	return context.WithValue(ctx, traceKey, trace)
}

// GetTraceFromContext returns the trace from the context
func GetTraceFromContext(ctx context.Context) string {
	trace, ok := ctx.Value(traceKey).(string)
	if !ok {
		return ""
	}
	return trace
}
