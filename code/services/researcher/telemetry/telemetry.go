// Package telemetry provides the researcher service's internal, in-process
// observability feed: it instruments agent tool calls (OTel spans + structured
// logs) and streams start/end events to any connected /telemetry SSE clients.
//
// This is distinct from the tracing package, which configures the
// OpenTelemetry SDK itself.
package telemetry

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
)

// Event is a single tool-execution lifecycle event streamed to SSE clients.
type Event struct {
	SessionID string `json:"session_id,omitempty"`
	Event     string `json:"event"` // "tool_start", "tool_end"
	Tool      string `json:"tool"`
	Duration  string `json:"duration,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// Broadcaster fans Events out to connected Server-Sent-Events clients.
// It is safe for concurrent use, and also serves as the /telemetry HTTP
// handler that clients subscribe through.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[chan Event]bool
}

// NewBroadcaster returns a ready-to-use Broadcaster.
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
			// Client is slow; drop the event rather than block the broadcaster.
		}
	}
}

// ServeHTTP streams Events as an SSE feed, optionally filtered to a single
// session via the "session_id" query parameter. In production this endpoint
// is protected by Cloud Run IAM; no additional application-level auth is
// enforced here.
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

	// Heartbeat
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
			// If session_id is provided, only stream events for that session.
			if sessionID != "" && event.SessionID != sessionID {
				continue
			}
			jsonData, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: telemetry\ndata: %s\n\n", jsonData)
			f.Flush()
		}
	}
}

// toolTiming tracks an in-flight tool call's start time and OTel span.
type toolTiming struct {
	start time.Time
	span  trace.Span
}

// ToolTracker instruments agent tool calls: it records OTel spans and
// structured logs, and publishes tool_start/tool_end Events to a Broadcaster.
// A ToolTracker's BeforeTool/AfterTool methods are meant to be wired in as
// an agent's llmagent.BeforeToolCallback/AfterToolCallback.
type ToolTracker struct {
	broadcaster *Broadcaster

	mu      sync.Mutex
	timings map[string]toolTiming
}

// NewToolTracker returns a ToolTracker that publishes to b.
func NewToolTracker(b *Broadcaster) *ToolTracker {
	return &ToolTracker{broadcaster: b, timings: make(map[string]toolTiming)}
}

func sessionIDFrom(ctx agent.Context) string {
	if ctx == nil {
		return ""
	}
	// ADK toolContext logs "Session() is not supported for tool context" if Session() is invoked.
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

// BeforeTool starts an OTel span and timer for the call, logs its start, and
// broadcasts a tool_start Event.
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

// AfterTool closes out the span and timer started by BeforeTool, logs the
// outcome, and broadcasts a tool_end Event.
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
