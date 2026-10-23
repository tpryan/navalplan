package tool

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

type mapState struct {
	mu   sync.RWMutex
	data map[string]any
}

func newMapState() *mapState {
	return &mapState{data: make(map[string]any)}
}

func (s *mapState) Get(key string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	return v, nil
}

func (s *mapState) Set(key string, val any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
	return nil
}

func (s *mapState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for k, v := range s.data {
			if !yield(k, v) {
				return
			}
		}
	}
}

type mockToolContext struct {
	agent.StrictContextMock
	state session.State
}

func newMockContext() *mockToolContext {
	return &mockToolContext{
		StrictContextMock: agent.NewStrictContextMock(context.Background()),
	}
}

func newStatefulMockContext() (*mockToolContext, *mapState) {
	st := newMapState()
	return &mockToolContext{
		StrictContextMock: agent.NewStrictContextMock(context.Background()),
		state:             st,
	}, st
}

func (m mockToolContext) Actions() *session.EventActions {
	return nil
}

func (m mockToolContext) FunctionCallID() string {
	return "test-call-id"
}

func (m mockToolContext) AgentName() string {
	return "test-agent"
}

func (m mockToolContext) AppName() string {
	return "test-app"
}

func (m mockToolContext) Artifacts() agent.Artifacts {
	return nil
}

func (m mockToolContext) Branch() string {
	return "test-branch"
}

func (m mockToolContext) InvocationID() string {
	return "test-invocation-id"
}

func (m mockToolContext) Agent() agent.Agent {
	return nil
}

func (m mockToolContext) Session() session.Session {
	return nil
}

func (m mockToolContext) ReadonlyState() session.ReadonlyState {
	return nil
}

func (m mockToolContext) SearchMemory(ctx context.Context, query string) (*memory.SearchResponse, error) {
	return nil, nil
}

func (m mockToolContext) SessionID() string {
	return "test-session-id"
}

func (m mockToolContext) State() session.State {
	return m.state
}

func (m mockToolContext) UserContent() *genai.Content {
	return nil
}

func (m mockToolContext) UserID() string {
	return "test-user-id"
}

func (m mockToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return nil
}

func (m mockToolContext) RequestConfirmation(hint string, payload any) error {
	return nil
}

func int32Ptr(i int32) *int32 {
	return &i
}
