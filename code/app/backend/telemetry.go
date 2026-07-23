package main

import (
	"context"
	"fmt"
	"log/slog"

	"os"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// InitTelemetry sets up OpenTelemetry for the application.
// In production, it exports traces to Google Cloud Trace.
func InitTelemetry(ctx context.Context, projectID, env string, disableTracing bool) (*sdktrace.TracerProvider, error) {
	if disableTracing {
		slog.Info("OTel tracing explicitly disabled")
		return nil, nil
	}

	resourceID := os.Getenv("NAVALPLAN_RESOURCE_ID")
	slog.Info("Telemetry initialization started",
		"projectID", projectID,
		"env", env,
		"NAVALPLAN_RESOURCE_ID", resourceID,
	)

	if env != "production" {
		slog.Info("OTel disabled in development environment")
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
	resAttrs := []attribute.KeyValue{
		attribute.String("service.name", "navalplan-backend"),
		attribute.String("deployment.environment", env),
		attribute.String("gcp.project_id", projectID),
	}

	// Manually inject cloud.resource_id if provided
	if resourceID != "" {
		slog.Info("Injecting cloud.resource_id into resource attributes", "id", resourceID)
		resAttrs = append(resAttrs, attribute.String("cloud.resource_id", resourceID))
		// Also add it without dots as a fallback
		resAttrs = append(resAttrs, attribute.String("cloud_resource_id", resourceID))
	}

	res, err := resource.New(ctx,
		resource.WithDetectors(gcp.NewDetector()),
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	// Log the final resource attributes for verification
	for _, attr := range res.Attributes() {
		slog.Debug("Resource attribute", "key", string(attr.Key), "value", attr.Value.Emit())
	}

	tp := sdktrace.NewTracerProvider(
		// Using SimpleSpanProcessor for immediate flushing during troubleshooting
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		sdktrace.WithResource(res),
	)

	// Set global OTel providers.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
		gcppropagator.CloudTraceFormatPropagator{},
	))

	slog.Info("OpenTelemetry initialized with Cloud Trace exporter and GCP propagator", "projectID", projectID)
	return tp, nil
}
