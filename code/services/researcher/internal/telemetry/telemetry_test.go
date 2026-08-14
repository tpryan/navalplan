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
func (r *syncRecorder) Flush()          {}

func (r *syncRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

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
	b.publish(Event{SessionID: "s1", Event: "tool_start", Tool: "GetTides"})
}

type fakeTool struct{ name string }

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "fake tool for tests" }
func (f *fakeTool) IsLongRunning() bool { return false }

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

func TestResolveProjectID(t *testing.T) {
	tests := []struct {
		name              string
		inputProjectID    string
		envVars           map[string]string
		expectedProjectID string
	}{
		{
			name:              "explicit parameter returned first",
			inputProjectID:    "my-explicit-project",
			envVars:           map[string]string{"GOOGLE_CLOUD_PROJECT": "env-project"},
			expectedProjectID: "my-explicit-project",
		},
		{
			name:              "fallback to GOOGLE_CLOUD_PROJECT",
			inputProjectID:    "",
			envVars:           map[string]string{"GOOGLE_CLOUD_PROJECT": "google-cloud-proj"},
			expectedProjectID: "google-cloud-proj",
		},
		{
			name:              "fallback to GCP_PROJECT",
			inputProjectID:    "",
			envVars:           map[string]string{"GCP_PROJECT": "gcp-proj"},
			expectedProjectID: "gcp-proj",
		},
		{
			name:              "fallback to GCLOUD_PROJECT",
			inputProjectID:    "",
			envVars:           map[string]string{"GCLOUD_PROJECT": "gcloud-proj"},
			expectedProjectID: "gcloud-proj",
		},
		{
			name:              "fallback to PROJECT_ID",
			inputProjectID:    "",
			envVars:           map[string]string{"PROJECT_ID": "proj-id"},
			expectedProjectID: "proj-id",
		},
		{
			name:           "ignore numeric project number in NAVALPLAN_RESOURCE_ID when GOOGLE_CLOUD_PROJECT set",
			inputProjectID: "",
			envVars: map[string]string{
				"GOOGLE_CLOUD_PROJECT":  "navallog",
				"NAVALPLAN_RESOURCE_ID": "projects/70159681032/locations/us-central1/reasoningEngines/1643463669536784384",
			},
			expectedProjectID: "navallog",
		},
		{
			name:           "fallback to NAVALPLAN_RESOURCE_ID extraction when string project ID present",
			inputProjectID: "",
			envVars: map[string]string{
				"NAVALPLAN_RESOURCE_ID": "projects/navallog/locations/us-central1/reasoningEngines/1643463669536784384",
			},
			expectedProjectID: "navallog",
		},
		{
			name:           "fallback to OTEL_RESOURCE_ATTRIBUTES extraction",
			inputProjectID: "",
			envVars: map[string]string{
				"OTEL_RESOURCE_ATTRIBUTES": "cloud.resource_id=projects/navallog/locations/us-central1/reasoningEngines/12345",
			},
			expectedProjectID: "navallog",
		},
		{
			name:              "empty when no env vars or parameter provided",
			inputProjectID:    "",
			envVars:           map[string]string{},
			expectedProjectID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{
				"GOOGLE_CLOUD_PROJECT", "GCP_PROJECT", "GCLOUD_PROJECT", "PROJECT_ID",
				"NAVALPLAN_RESOURCE_ID", "OTEL_RESOURCE_ATTRIBUTES",
			} {
				t.Setenv(k, "")
			}
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}
			res := ResolveProjectID(tt.inputProjectID)
			if res != tt.expectedProjectID {
				t.Errorf("ResolveProjectID(%q) = %q, want %q", tt.inputProjectID, res, tt.expectedProjectID)
			}
		})
	}
}
