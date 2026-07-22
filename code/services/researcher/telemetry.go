package main

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
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

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("navalplan-researcher"),
			semconv.DeploymentEnvironmentKey.String(env),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	// Use ADK's official telemetry setup which hooks into telemetry.googleapis.com
	// for Agent Platform metrics (Invocations, Sessions, Model Calls).
	telemetryProviders, err := telemetry.New(ctx,
		telemetry.WithOtelToCloud(true),
		telemetry.WithResource(res),
		telemetry.WithGcpResourceProject(projectID),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ADK telemetry: %w", err)
	}

	// Register as global OTel providers
	telemetryProviders.SetGlobalOtelProviders()

	slog.Info("Agent Platform telemetry initialized", "projectID", projectID)
	return telemetryProviders, nil
}
