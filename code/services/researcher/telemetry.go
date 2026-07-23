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
	slog.Info("Telemetry initialization",
		"projectID", projectID,
		"env", env,
		"NAVALPLAN_RESOURCE_ID", resourceID,
		"OTEL_RESOURCE_ATTRIBUTES", os.Getenv("OTEL_RESOURCE_ATTRIBUTES"),
	)

	// 1. Create the resource with necessary attributes
	detector := gcp.NewDetector()
	resAttrs := []attribute.KeyValue{
		attribute.String("service.name", "navalplan-researcher"),
		attribute.String("deployment.environment", env),
	}

	// Manually inject cloud.resource_id if provided (critical for BigQuery Agent Analytics)
	if resourceID != "" {
		resAttrs = append(resAttrs, attribute.String("cloud.resource_id", resourceID))
	}

	res, err := resource.New(ctx,
		resource.WithDetectors(detector),
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	// Initialize Cloud Trace exporter to ensure data reaches the BigQuery export table.
	// ADK's default OtelToCloud sends to telemetry.googleapis.com, but standard BQ Trace export
	// listens to trace.googleapis.com.
	traceExporter, err := texporter.New(texporter.WithProjectID(projectID))
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud Trace exporter: %w", err)
	}

	// Use ADK's official telemetry setup which hooks into telemetry.googleapis.com
	// for Agent Platform metrics (Invocations, Sessions, Model Calls).
	telemetryProviders, err := telemetry.New(ctx,
		telemetry.WithOtelToCloud(true),
		telemetry.WithResource(res),
		telemetry.WithGcpResourceProject(projectID),
		telemetry.WithGenAICaptureMessageContent(true),
		telemetry.WithSpanProcessors(sdktrace.NewBatchSpanProcessor(traceExporter)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ADK telemetry: %w", err)
	}

	// Register as global OTel providers and set the standard trace propagator
	telemetryProviders.SetGlobalOtelProviders()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
		gcppropagator.CloudTraceFormatPropagator{},
	))

	slog.Info("Agent Platform telemetry initialized", "projectID", projectID)
	return telemetryProviders, nil
}
