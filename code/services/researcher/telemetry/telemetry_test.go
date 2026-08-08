package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

// syncRecorder is a minimal, concurrency-safe http.ResponseWriter +
// http.Flusher, since httptest.ResponseRecorder isn't safe for the
// concurrent write (from the handler goroutine) / read (from the test
// goroutine) pattern SSE tests need.
type syncRecorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
	hdr http.Header
}

func newSyncRecorder() *syncRecorder {
	return &syncRecorder{hdr: make(http.Header)}
}

func (r *syncRecorder) Header() http.Header { return r.hdr }

func (r *syncRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *syncRecorder) WriteHeader(int) {}

func (r *syncRecorder) Flush() {}

func (r *syncRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// waitForBody retries publish until substr appears in rec, or fails the test
// after timeout. Retrying is necessary because the subscriber goroutine
// needs a moment to register itself before it can receive a broadcast.
func waitForBody(t *testing.T, rec *syncRecorder, publish func(), substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		publish()
		if strings.Contains(rec.String(), substr) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in SSE body; got: %q", substr, rec.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBroadcaster_ServeHTTP_StreamsEvents(t *testing.T) {
	b := NewBroadcaster()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/telemetry", nil).WithContext(ctx)
	rec := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(rec, req)
		close(done)
	}()

	event := Event{SessionID: "s1", Event: "tool_start", Tool: "GetTides", Timestamp: 123}
	waitForBody(t, rec, func() { b.publish(event) }, `"tool":"GetTides"`, 2*time.Second)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeHTTP did not return after request context was canceled")
	}
}

func TestBroadcaster_ServeHTTP_FiltersBySessionID(t *testing.T) {
	b := NewBroadcaster()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/telemetry?session_id=wanted", nil).WithContext(ctx)
	rec := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(rec, req)
		close(done)
	}()

	// Events for a different session should never show up.
	other := Event{SessionID: "other", Event: "tool_start", Tool: "ShouldNotAppear", Timestamp: 1}
	wanted := Event{SessionID: "wanted", Event: "tool_start", Tool: "ShouldAppear", Timestamp: 2}

	waitForBody(t, rec, func() {
		b.publish(other)
		b.publish(wanted)
	}, `"tool":"ShouldAppear"`, 2*time.Second)

	if strings.Contains(rec.String(), "ShouldNotAppear") {
		t.Errorf("event for a different session leaked through the session_id filter: %q", rec.String())
	}
}

func TestBroadcaster_Publish_NoSubscribersIsNoop(t *testing.T) {
	b := NewBroadcaster()
	// Should not panic or block even though nothing is subscribed.
	b.publish(Event{SessionID: "s1", Event: "tool_start", Tool: "GetTides"})
}

// fakeTool is a minimal tool.Tool for exercising ToolTracker.
type fakeTool struct{ name string }

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "fake tool for tests" }
func (f *fakeTool) IsLongRunning() bool  { return false }

// mockContext is a minimal agent.Context: it embeds agent.StrictContextMock
// (whose un-overridden methods panic with "not implemented") and overrides
// only what ToolTracker actually calls.
type mockContext struct {
	agent.StrictContextMock
	functionCallID string
	session        session.Session
}

func newMockContext(functionCallID string) *mockContext {
	return &mockContext{
		StrictContextMock: agent.NewStrictContextMock(context.Background()),
		functionCallID:    functionCallID,
	}
}

func (m *mockContext) FunctionCallID() string   { return m.functionCallID }
func (m *mockContext) Session() session.Session { return m.session }

func TestToolTracker_BeforeAfter_PassesResultThrough(t *testing.T) {
	tracker := NewToolTracker(NewBroadcaster())
	ctx := newMockContext("call-1")
	tl := &fakeTool{name: "GetTides"}

	if _, err := tracker.BeforeTool(ctx, tl, map[string]any{"foo": "bar"}); err != nil {
		t.Fatalf("BeforeTool() error = %v", err)
	}

	result := map[string]any{"ok": true}
	got, err := tracker.AfterTool(ctx, tl, map[string]any{"foo": "bar"}, result, nil)
	if err != nil {
		t.Fatalf("AfterTool() error = %v", err)
	}
	if got["ok"] != true {
		t.Errorf("AfterTool() should pass the result through unchanged, got %v", got)
	}
}

func TestToolTracker_AfterTool_WithoutBeforeTool_DoesNotPanic(t *testing.T) {
	tracker := NewToolTracker(NewBroadcaster())
	ctx := newMockContext("never-started")
	tl := &fakeTool{name: "GetWeather"}

	// AfterTool for a call ID that never went through BeforeTool (e.g. timing
	// map cleared, or a race) should degrade gracefully rather than panic.
	got, err := tracker.AfterTool(ctx, tl, nil, map[string]any{"ok": true}, nil)
	if err != nil {
		t.Fatalf("AfterTool() error = %v", err)
	}
	if got["ok"] != true {
		t.Errorf("AfterTool() should still pass the result through, got %v", got)
	}
}

func TestToolTracker_BroadcastsStartAndEnd(t *testing.T) {
	b := NewBroadcaster()
	tracker := NewToolTracker(b)
	ctx := newMockContext("call-2")
	tl := &fakeTool{name: "GetSunriseSunset"}

	httpCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/telemetry", nil).WithContext(httpCtx)
	rec := newSyncRecorder()

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(rec, req)
		close(done)
	}()

	// Retry BeforeTool/AfterTool until the subscriber has registered and
	// observed both the tool_start and tool_end events.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := tracker.BeforeTool(ctx, tl, nil); err != nil {
			t.Fatalf("BeforeTool() error = %v", err)
		}
		if _, err := tracker.AfterTool(ctx, tl, nil, map[string]any{}, nil); err != nil {
			t.Fatalf("AfterTool() error = %v", err)
		}
		body := rec.String()
		if strings.Contains(body, `"event":"tool_start"`) && strings.Contains(body, `"event":"tool_end"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for tool_start/tool_end events; got: %q", body)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeHTTP did not return after request context was canceled")
	}
}
