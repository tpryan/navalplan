// Package tracing configures OpenTelemetry for the researcher service.
package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/adk/v2/telemetry"
)

type resourceIDSpanProcessor struct {
	resourceID string
}

func (p *resourceIDSpanProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	if p.resourceID != "" {
		s.SetAttributes(
			attribute.String("cloud.resource_id", p.resourceID),
			attribute.String("cloud_resource_id", p.resourceID),
		)
	}
}

func (p *resourceIDSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan)        {}
func (p *resourceIDSpanProcessor) Shutdown(ctx context.Context) error   { return nil }
func (p *resourceIDSpanProcessor) ForceFlush(ctx context.Context) error { return nil }

// ResolveProjectID resolves the GCP project ID from the input string,
// environment variables, resource strings, or GCP metadata server.
func ResolveProjectID(projectID string) string {
	if pid := strings.TrimSpace(projectID); pid != "" {
		return pid
	}

	for _, key := range []string{"GOOGLE_CLOUD_PROJECT", "GCP_PROJECT", "GCLOUD_PROJECT", "PROJECT_ID"} {
		if pid := strings.TrimSpace(os.Getenv(key)); pid != "" {
			return pid
		}
	}

	for _, raw := range []string{os.Getenv("NAVALPLAN_RESOURCE_ID"), os.Getenv("OTEL_RESOURCE_ATTRIBUTES")} {
		if idx := strings.Index(raw, "projects/"); idx != -1 {
			sub := raw[idx+len("projects/"):]
			if slashIdx := strings.IndexByte(sub, '/'); slashIdx > 0 {
				if pid := strings.TrimSpace(sub[:slashIdx]); pid != "" {
					return pid
				}
			}
		}
	}

	if metadata.OnGCE() {
		if pid, err := metadata.ProjectID(); err == nil && strings.TrimSpace(pid) != "" {
			return strings.TrimSpace(pid)
		}
	}

	return ""
}

// Init sets up OpenTelemetry for the researcher service.
// It configures two exporters:
// 1. The default ADK exporter (sends data to telemetry.googleapis.com)
// 2. A custom Cloud Trace exporter (sends data to trace.googleapis.com for BQ Agent Analytics)
func Init(ctx context.Context, projectID, env string, disableTracing bool) (*telemetry.Providers, error) {
	if disableTracing {
		slog.Info("OTel tracing explicitly disabled")
		return nil, nil
	}

	projectID = ResolveProjectID(projectID)

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
		resourceID = fmt.Sprintf("projects/%s/locations/us-central1/reasoningEngines/1643463669536784384", projectID)
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

	detector := gcp.NewDetector()
	resAttrs := []attribute.KeyValue{
		attribute.String("service.name", "navalplan-researcher"),
		attribute.String("deployment.environment", env),
	}
	if projectID != "" {
		resAttrs = append(resAttrs, attribute.String("gcp.project_id", projectID))
	}

	if resourceID != "" {
		slog.Info("Injecting cloud.resource_id into resource attributes", "id", resourceID)
		resAttrs = append(resAttrs, attribute.String("cloud.resource_id", resourceID))
		resAttrs = append(resAttrs, attribute.String("cloud_resource_id", resourceID))
	}

	resCtx, resCancel := context.WithTimeout(ctx, 5*time.Second)
	defer resCancel()

	// Detect GCP environment attributes (e.g. region, instance ID)
	gcpRes, _ := resource.New(resCtx, resource.WithDetectors(detector))

	// Define explicit overrides (Reasoning Engine resource_id, project_id, service.name)
	overrideRes, err := resource.New(resCtx,
		resource.WithFromEnv(),
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel override resource: %w", err)
	}

	// Merge overrideRes OVER gcpRes so Reasoning Engine URI overrides Cloud Run revision ID!
	res, err := resource.Merge(gcpRes, overrideRes)
	if err != nil {
		return nil, fmt.Errorf("failed to merge OTel resources: %w", err)
	}

	for _, attr := range res.Attributes() {
		slog.Debug("Resource attribute", "key", string(attr.Key), "value", attr.Value.Emit())
	}

	var exporterOpts []texporter.Option
	if projectID != "" {
		exporterOpts = append(exporterOpts, texporter.WithProjectID(projectID))
	}

	traceExporter, err := texporter.New(exporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud Trace exporter: %w", err)
	}

	spanProcessor := &resourceIDSpanProcessor{resourceID: resourceID}

	adkOpts := []telemetry.Option{
		telemetry.WithOtelToCloud(true),
		telemetry.WithResource(res),
		telemetry.WithSpanProcessors(spanProcessor, sdktrace.NewBatchSpanProcessor(traceExporter)),
	}
	if projectID != "" {
		adkOpts = append(adkOpts, telemetry.WithGcpResourceProject(projectID))
	}

	telemetryProviders, err := telemetry.New(ctx, adkOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ADK telemetry: %w", err)
	}

	telemetryProviders.SetGlobalOtelProviders()

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
		gcppropagator.CloudTraceFormatPropagator{},
	))

	slog.Info("Agent Platform and Cloud Trace telemetry initialized", "projectID", projectID, "resourceID", resourceID)
	return telemetryProviders, nil
}
