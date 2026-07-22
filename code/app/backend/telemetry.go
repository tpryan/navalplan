package main

import (
	"context"
	"fmt"
	"log/slog"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// InitTelemetry sets up OpenTelemetry for the application.
// In production, it exports traces to Google Cloud Trace.
func InitTelemetry(ctx context.Context, projectID, env string, disableTracing bool) (*sdktrace.TracerProvider, error) {
	if env != "production" || disableTracing {
		if disableTracing {
			slog.Info("OTel tracing explicitly disabled")
		} else {
			slog.Info("OTel disabled in development environment")
		}
		return nil, nil
	}

	if projectID == "" {
		slog.Warn("OTel project ID is empty, traces may not be exported correctly")
	}

	exporter, err := texporter.New(texporter.WithProjectID(projectID))
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud Trace exporter: %w", err)
	}

	// Use the GCP detector to automatically populate resource attributes (e.g. instance ID, region)
	res, err := resource.New(ctx,
		resource.WithDetectors(gcp.NewDetector()),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("navalplan-backend"),
			semconv.DeploymentEnvironmentKey.String(env),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set global OTel providers.
	// We use a composite propagator that supports both standard W3C TraceContext
	// and GCP's X-Cloud-Trace-Context.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
		gcppropagator.CloudTraceFormatPropagator{},
	))

	slog.Info("OpenTelemetry initialized with Cloud Trace exporter and GCP propagator", "projectID", projectID)
	return tp, nil
}
