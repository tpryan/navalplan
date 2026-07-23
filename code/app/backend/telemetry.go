package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

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

	resourceID := strings.TrimSpace(os.Getenv("NAVALPLAN_RESOURCE_ID"))
	if resourceID == "" {
		otelAttrs := os.Getenv("OTEL_RESOURCE_ATTRIBUTES")
		for _, kv := range strings.Split(otelAttrs, ",") {
			parts := strings.SplitN(strings.TrimSpace(kv), "=", 2)
			if len(parts) == 2 && parts[0] == "cloud.resource_id" {
				resourceID = strings.TrimSpace(parts[1])
				break
			}
		}
	}
	if resourceID == "" && projectID != "" && env == "production" {
		resourceID = fmt.Sprintf("projects/%s/locations/us-central1/services/navalplan-backend", projectID)
	}

	slog.Info("Telemetry initialization started",
		"projectID", projectID,
		"env", env,
		"resolved_resourceID", resourceID,
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

	resAttrs := []attribute.KeyValue{
		attribute.String("service.name", "navalplan-backend"),
		attribute.String("deployment.environment", env),
		attribute.String("gcp.project_id", projectID),
	}

	if resourceID != "" {
		slog.Info("Injecting cloud.resource_id into resource attributes", "id", resourceID)
		resAttrs = append(resAttrs, attribute.String("cloud.resource_id", resourceID))
		resAttrs = append(resAttrs, attribute.String("cloud_resource_id", resourceID))
	}

	resCtx, resCancel := context.WithTimeout(ctx, 5*time.Second)
	defer resCancel()

	res, err := resource.New(resCtx,
		resource.WithDetectors(gcp.NewDetector()),
		resource.WithFromEnv(),
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	for _, attr := range res.Attributes() {
		slog.Debug("Resource attribute", "key", string(attr.Key), "value", attr.Value.Emit())
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
		gcppropagator.CloudTraceFormatPropagator{},
	))

	slog.Info("OpenTelemetry initialized with Cloud Trace exporter and GCP propagator", "projectID", projectID, "resourceID", resourceID)
	return tp, nil
}
