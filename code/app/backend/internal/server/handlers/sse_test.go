package handlers

import (
	"testing"
	"time"

	"app/internal/model"
)

func TestRecStreams_PerInstance(t *testing.T) {
	h1 := newHandlerForSSETest()
	h2 := newHandlerForSSETest()

	sessionID := "test-session"
	rec := model.VoyageRecommendation{Name: "Test Anchorage"}

	ch := make(chan model.VoyageRecommendation, 1)
	h1.muRecStreams.Lock()
	h1.recStreams[sessionID] = ch
	h1.muRecStreams.Unlock()

	h1.broadcastRecommendation(sessionID, rec)

	select {
	case got := <-ch:
		if got.Name != rec.Name {
			t.Errorf("got recommendation %q, want %q", got.Name, rec.Name)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected recommendation on h1 channel, got none")
	}

	h2.broadcastRecommendation(sessionID, rec)

	select {
	case unexpected := <-ch:
		t.Errorf("h2 broadcast leaked into h1 channel: got %q", unexpected.Name)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRecStreams_Broadcast_DropsWhenFull(t *testing.T) {
	h := newHandlerForSSETest()

	sessionID := "full-session"
	ch := make(chan model.VoyageRecommendation, 1)
	h.muRecStreams.Lock()
	h.recStreams[sessionID] = ch
	h.muRecStreams.Unlock()

	rec := model.VoyageRecommendation{Name: "First"}
	h.broadcastRecommendation(sessionID, rec)

	done := make(chan struct{})
	go func() {
		h.broadcastRecommendation(sessionID, model.VoyageRecommendation{Name: "Second"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("broadcastRecommendation blocked on a full channel")
	}
}

func TestRecStreams_NilMap_DoesNotPanic(t *testing.T) {
	h := &Handler{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("broadcastRecommendation panicked on nil map: %v", r)
		}
	}()
	h.broadcastRecommendation("any-session", model.VoyageRecommendation{Name: "X"})
}

func TestEnsureRecChannel(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		ttl       time.Duration
	}{
		{
			name:      "creates new buffered channel",
			sessionID: "sess-1",
			ttl:       1 * time.Minute,
		},
		{
			name:      "reuses existing channel",
			sessionID: "sess-2",
			ttl:       1 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandlerForSSETest()
			ch1 := h.ensureRecChannel(tt.sessionID, tt.ttl)
			if cap(ch1) < 100 {
				t.Errorf("expected channel cap >= 100, got %d", cap(ch1))
			}
			ch2 := h.ensureRecChannel(tt.sessionID, tt.ttl)
			if ch1 != ch2 {
				t.Errorf("ensureRecChannel returned different channels for same session")
			}
		})
	}
}

func newHandlerForSSETest() *Handler {
	return &Handler{
		recStreams:  make(map[string]chan model.VoyageRecommendation),
		ResearchSem: make(chan struct{}, 10),
	}
}
