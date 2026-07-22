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
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/adk/telemetry"
)

// InitTelemetry sets up OpenTelemetry for the application using the official ADK telemetry package.
// This ensures that model calls and agent metrics are correctly reported to the Agent Platform dashboard.
func InitTelemetry(ctx context.Context, projectID, env string, disableTracing bool) (*telemetry.Providers, error) {
	if env != "production" || disableTracing {
		if disableTracing {
			slog.Info("OTel tracing explicitly disabled")
		} else {
			slog.Info("OTel disabled in development environment")
		}
		return nil, nil
	}

	// Use GCP detector to automatically populate resource attributes (project, region, instance, etc.)
	detector := gcp.NewDetector()
	resAttrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String("navalplan-researcher"),
		semconv.DeploymentEnvironmentKey.String(env),
	}

	// Manually inject cloud.resource_id if provided (critical for BigQuery Agent Analytics)
	if resourceID := os.Getenv("NAVALPLAN_RESOURCE_ID"); resourceID != "" {
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
