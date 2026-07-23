package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"os"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/adk/telemetry"
)

// InitTelemetry sets up OpenTelemetry for the researcher service.
// It configures two exporters:
// 1. The default ADK exporter (sends data to telemetry.googleapis.com)
// 2. A custom Cloud Trace exporter (sends data to trace.googleapis.com for BQ Agent Analytics)
func InitTelemetry(ctx context.Context, projectID, env string, disableTracing bool) (*telemetry.Providers, error) {
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

	// 1. Create the resource with necessary attributes
	detector := gcp.NewDetector()
	resAttrs := []attribute.KeyValue{
		attribute.String("service.name", "navalplan-researcher"),
		attribute.String("deployment.environment", env),
		attribute.String("gcp.project_id", projectID),
	}

	// Manually inject cloud.resource_id if provided (critical for BigQuery Agent Analytics)
	if resourceID != "" {
		slog.Info("Injecting cloud.resource_id into resource attributes", "id", resourceID)
		resAttrs = append(resAttrs, attribute.String("cloud.resource_id", resourceID))
		// Also add it without dots as a fallback
		resAttrs = append(resAttrs, attribute.String("cloud_resource_id", resourceID))
	}

	// 1.5 Add a timeout for resource detection to prevent hanging if GCP metadata is slow
	resCtx, resCancel := context.WithTimeout(ctx, 5*time.Second)
	defer resCancel()

	res, err := resource.New(resCtx,
		resource.WithDetectors(detector),
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	// Log the final resource attributes for verification in Cloud Logging
	for _, attr := range res.Attributes() {
		slog.Debug("Resource attribute", "key", string(attr.Key), "value", attr.Value.Emit())
	}

	// Initialize Cloud Trace exporter to ensure data reaches the BigQuery export table.
	traceExporter, err := texporter.New(texporter.WithProjectID(projectID))
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud Trace exporter: %w", err)
	}

	// Use ADK's official telemetry setup
	telemetryProviders, err := telemetry.New(ctx,
		telemetry.WithOtelToCloud(true),
		telemetry.WithResource(res),
		telemetry.WithGcpResourceProject(projectID),
		telemetry.WithGenAICaptureMessageContent(true),
		// Using BatchSpanProcessor for production to avoid blocking request threads
		telemetry.WithSpanProcessors(sdktrace.NewBatchSpanProcessor(traceExporter)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ADK telemetry: %w", err)
	}

	// Register as global OTel providers
	telemetryProviders.SetGlobalOtelProviders()

	// Set the standard trace propagator with Cloud Trace support
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
		gcppropagator.CloudTraceFormatPropagator{},
	))

	slog.Info("Agent Platform and Cloud Trace telemetry initialized", "projectID", projectID)
	return telemetryProviders, nil
}
