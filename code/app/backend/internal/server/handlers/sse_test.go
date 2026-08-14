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

func newHandlerForSSETest() *Handler {
	return &Handler{
		recStreams:  make(map[string]chan model.VoyageRecommendation),
		ResearchSem: make(chan struct{}, 10),
	}
}
