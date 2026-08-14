package telemetry

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
)

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// resolveProjectID resolves the GCP project ID from the input string,
// environment variables, resource strings, or GCP metadata server.
func resolveProjectID(projectID string) string {
	if pid := strings.TrimSpace(projectID); pid != "" && !isNumeric(pid) {
		return pid
	}

	for _, key := range []string{"GOOGLE_CLOUD_PROJECT", "GCP_PROJECT", "GCLOUD_PROJECT", "PROJECT_ID"} {
		if pid := strings.TrimSpace(os.Getenv(key)); pid != "" && !isNumeric(pid) {
			return pid
		}
	}

	if metadata.OnGCE() {
		if pid, err := metadata.ProjectID(); err == nil && strings.TrimSpace(pid) != "" && !isNumeric(pid) {
			return strings.TrimSpace(pid)
		}
	}

	for _, raw := range []string{os.Getenv("NAVALPLAN_RESOURCE_ID"), os.Getenv("OTEL_RESOURCE_ATTRIBUTES")} {
		if idx := strings.Index(raw, "projects/"); idx != -1 {
			sub := raw[idx+len("projects/"):]
			if slashIdx := strings.IndexByte(sub, '/'); slashIdx > 0 {
				if pid := strings.TrimSpace(sub[:slashIdx]); pid != "" && !isNumeric(pid) {
					return pid
				}
			}
		}
	}

	if pid := strings.TrimSpace(projectID); pid != "" {
		return pid
	}
	for _, key := range []string{"GOOGLE_CLOUD_PROJECT", "GCP_PROJECT", "GCLOUD_PROJECT", "PROJECT_ID"} {
		if pid := strings.TrimSpace(os.Getenv(key)); pid != "" {
			return pid
		}
	}

	return ""
}

// InitTelemetry sets up OpenTelemetry for the application.
// In production, it exports traces to Google Cloud Trace.
func InitTelemetry(ctx context.Context, projectID, env string, disableTracing bool) (*sdktrace.TracerProvider, error) {
	if disableTracing {
		slog.Info("OTel tracing explicitly disabled")
		return nil, nil
	}

	projectID = resolveProjectID(projectID)

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

	var exporterOpts []texporter.Option
	if projectID != "" {
		exporterOpts = append(exporterOpts, texporter.WithProjectID(projectID))
	}

	exporter, err := texporter.New(exporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud Trace exporter: %w", err)
	}

	resAttrs := []attribute.KeyValue{
		attribute.String("service.name", "navalplan-backend"),
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

	res, err := resource.New(resCtx,
		resource.WithDetectors(gcp.NewDetector()),
		resource.WithFromEnv(),
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
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
