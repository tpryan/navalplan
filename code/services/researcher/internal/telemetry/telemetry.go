// Package telemetry provides logging, tracing, and internal tool telemetry for researcher.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"github.com/charmbracelet/lipgloss"
	charm "github.com/charmbracelet/log"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/telemetry"
	"google.golang.org/adk/v2/tool"
)

type key int

const (
	traceKey key = iota
	spanKey
)

// AddTraceToContext adds the trace to the context.
func AddTraceToContext(ctx context.Context, tr string) context.Context {
	return context.WithValue(ctx, traceKey, tr)
}

// GetTraceFromContext returns the trace from the context.
func GetTraceFromContext(ctx context.Context) string {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		return span.TraceID().String()
	}
	tr, ok := ctx.Value(traceKey).(string)
	if !ok {
		return ""
	}
	return tr
}

// AddSpanToContext adds the span ID to the context.
func AddSpanToContext(ctx context.Context, sp string) context.Context {
	return context.WithValue(ctx, spanKey, sp)
}

// GetSpanFromContext returns the span ID from the context.
func GetSpanFromContext(ctx context.Context) string {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		return span.SpanID().String()
	}
	sp, ok := ctx.Value(spanKey).(string)
	if !ok {
		return ""
	}
	return sp
}

// InitLogging sets up the global slog logger based on environment.
func InitLogging(env string) {
	var handler slog.Handler

	if env == "production" {
		jsonHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			AddSource: true,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				switch a.Key {
				case slog.MessageKey:
					a.Key = "message"
				case slog.SourceKey:
					a.Key = "logging.googleapis.com/sourceLocation"
				case slog.LevelKey:
					a.Key = "severity"
				}
				return a
			},
		})
		handler = &CloudLoggingHandler{Handler: jsonHandler, FormatMessage: false}
	} else {
		lipgloss.SetHasDarkBackground(true)
		chOptions := charm.Options{Prefix: "agent", ReportTimestamp: true, Level: charm.DebugLevel}
		cbLogger := charm.NewWithOptions(os.Stderr, chOptions)
		handler = &CloudLoggingHandler{Handler: cbLogger, FormatMessage: true}
	}

	slog.SetDefault(slog.New(handler))
}

type CloudLoggingHandler struct {
	Handler       slog.Handler
	FormatMessage bool
}

var _ slog.Handler = (*CloudLoggingHandler)(nil)

func (h *CloudLoggingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h *CloudLoggingHandler) Handle(ctx context.Context, r slog.Record) error {
	if !h.FormatMessage {
		if tr := GetTraceFromContext(ctx); tr != "" {
			r.Add("logging.googleapis.com/trace", slog.StringValue(tr))
		}
		if sp := GetSpanFromContext(ctx); sp != "" {
			r.Add("logging.googleapis.com/spanId", slog.StringValue(sp))
		}
		return h.Handler.Handle(ctx, r)
	}

	var styledDur string
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "duration" {
			durStr := fmt.Sprintf("%v", a.Value.Any())
			dur, err := time.ParseDuration(durStr)
			if err == nil {
				var color string
				switch {
				case dur < time.Millisecond:
					color = "255"
				case dur < time.Second:
					color = "226"
				case dur < time.Minute:
					color = "208"
				default:
					color = "196"
				}
				styledDur = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(durStr)
				return true
			}
		}
		newRecord.AddAttrs(a)
		return true
	})

	var sb strings.Builder
	sb.WriteString(newRecord.Message)

	if styledDur != "" {
		sb.WriteString(" ")
		keyStyle := lipgloss.NewStyle().Bold(true)
		sb.WriteString(keyStyle.Render("duration"))
		sb.WriteString("=")
		sb.WriteString(styledDur)
	}

	newRecord.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(fmt.Sprintf("%v", a.Value.Any()))
		return true
	})
	newRecord.Message = sb.String()

	if tr := GetTraceFromContext(ctx); tr != "" {
		newRecord.Add("logging.googleapis.com/trace", slog.StringValue(tr))
	}
	if sp := GetSpanFromContext(ctx); sp != "" {
		newRecord.Add("logging.googleapis.com/spanId", slog.StringValue(sp))
	}
	return h.Handler.Handle(ctx, newRecord)
}

func (h *CloudLoggingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CloudLoggingHandler{Handler: h.Handler.WithAttrs(attrs), FormatMessage: h.FormatMessage}
}

func (h *CloudLoggingHandler) WithGroup(name string) slog.Handler {
	return &CloudLoggingHandler{Handler: h.Handler.WithGroup(name), FormatMessage: h.FormatMessage}
}

// Event represents a tool execution lifecycle event.
type Event struct {
	SessionID string `json:"session_id,omitempty"`
	Event     string `json:"event"`
	Tool      string `json:"tool"`
	Duration  string `json:"duration,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// Broadcaster fans Events out to connected SSE clients.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[chan Event]bool
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{clients: make(map[chan Event]bool)}
}

func (b *Broadcaster) publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for client := range b.clients {
		select {
		case client <- event:
		default:
		}
	}
}

func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	sessionID := r.URL.Query().Get("session_id")

	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	messageChan := make(chan Event, 10)
	b.mu.Lock()
	b.clients[messageChan] = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, messageChan)
		close(messageChan)
		b.mu.Unlock()
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {}\n\n")
			f.Flush()
		case event := <-messageChan:
			if sessionID != "" && event.SessionID != sessionID {
				continue
			}
			jsonData, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: telemetry\ndata: %s\n\n", jsonData)
			f.Flush()
		}
	}
}

type toolTiming struct {
	start time.Time
	span  trace.Span
}

type ToolTracker struct {
	broadcaster *Broadcaster
	mu          sync.Mutex
	timings     map[string]toolTiming
}

func NewToolTracker(b *Broadcaster) *ToolTracker {
	return &ToolTracker{broadcaster: b, timings: make(map[string]toolTiming)}
}

func sessionIDFrom(ctx agent.Context) string {
	if ctx == nil {
		return ""
	}
	typeStr := fmt.Sprintf("%T", ctx)
	if strings.Contains(strings.ToLower(typeStr), "tool") {
		return ""
	}
	defer func() {
		_ = recover()
	}()
	if sess := ctx.Session(); sess != nil {
		return sess.ID()
	}
	return ""
}

func (t *ToolTracker) BeforeTool(ctx agent.Context, tl tool.Tool, args map[string]any) (map[string]any, error) {
	_, span := otel.Tracer("navalplan-researcher").Start(ctx, "tool:"+tl.Name())

	t.mu.Lock()
	t.timings[ctx.FunctionCallID()] = toolTiming{start: time.Now(), span: span}
	t.mu.Unlock()

	sessionID := sessionIDFrom(ctx)
	slog.Log(ctx, slog.LevelInfo, "tool_start",
		"tool", tl.Name(),
		"args", args,
		"function_call_id", ctx.FunctionCallID(),
		"session_id", sessionID,
	)

	t.broadcaster.publish(Event{
		SessionID: sessionID,
		Event:     "tool_start",
		Tool:      tl.Name(),
		Timestamp: time.Now().UnixMilli(),
	})
	return nil, nil
}

func (t *ToolTracker) AfterTool(ctx agent.Context, tl tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
	t.mu.Lock()
	timing, ok := t.timings[ctx.FunctionCallID()]
	if ok {
		delete(t.timings, ctx.FunctionCallID())
	}
	t.mu.Unlock()

	sessionID := sessionIDFrom(ctx)
	var duration string
	if ok {
		duration = time.Since(timing.start).String()
		status := "success"
		if err != nil {
			status = "error"
			timing.span.RecordError(err)
		}
		timing.span.End()

		slog.Log(ctx, slog.LevelInfo, "tool_end",
			"tool", tl.Name(),
			"duration", duration,
			"status", status,
			"result", result,
			"error", err,
			"function_call_id", ctx.FunctionCallID(),
			"session_id", sessionID,
		)
	}

	t.broadcaster.publish(Event{
		SessionID: sessionID,
		Event:     "tool_end",
		Tool:      tl.Name(),
		Duration:  duration,
		Timestamp: time.Now().UnixMilli(),
	})

	return result, nil
}

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

func ResolveProjectID(projectID string) string {
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

func InitTracing(ctx context.Context, projectID, env string, disableTracing bool) (*telemetry.Providers, error) {
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

	gcpRes, _ := resource.New(resCtx, resource.WithDetectors(detector))
	overrideRes, err := resource.New(resCtx,
		resource.WithFromEnv(),
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel override resource: %w", err)
	}

	res, err := resource.Merge(gcpRes, overrideRes)
	if err != nil {
		return nil, fmt.Errorf("failed to merge OTel resources: %w", err)
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
