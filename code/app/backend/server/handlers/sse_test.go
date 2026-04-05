package handlers

import (
	"testing"
	"time"

	"app/models"
)

// TestRecStreams_PerInstance verifies that two separate Handler instances do not
// share SSE stream state — a broadcast on one must not reach the other.
func TestRecStreams_PerInstance(t *testing.T) {
	h1 := newHandlerForSSETest()
	h2 := newHandlerForSSETest()

	sessionID := "test-session"
	rec := models.VoyageRecommendation{Name: "Test Anchorage"}

	// Register a channel on h1 only.
	ch := make(chan models.VoyageRecommendation, 1)
	h1.muRecStreams.Lock()
	h1.recStreams[sessionID] = ch
	h1.muRecStreams.Unlock()

	// Broadcast via h1 — channel should receive.
	h1.broadcastRecommendation(sessionID, rec)

	select {
	case got := <-ch:
		if got.Name != rec.Name {
			t.Errorf("got recommendation %q, want %q", got.Name, rec.Name)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected recommendation on h1 channel, got none")
	}

	// Broadcast via h2 — h2 has no registered channel, so h1's channel must be silent.
	h2.broadcastRecommendation(sessionID, rec)

	select {
	case unexpected := <-ch:
		t.Errorf("h2 broadcast leaked into h1 channel: got %q", unexpected.Name)
	case <-time.After(50 * time.Millisecond):
		// Correct: nothing arrived.
	}
}

// TestRecStreams_Broadcast_DropsWhenFull verifies that a full channel does not block.
func TestRecStreams_Broadcast_DropsWhenFull(t *testing.T) {
	h := newHandlerForSSETest()

	sessionID := "full-session"
	// Buffer of 1 — fill it immediately.
	ch := make(chan models.VoyageRecommendation, 1)
	h.muRecStreams.Lock()
	h.recStreams[sessionID] = ch
	h.muRecStreams.Unlock()

	rec := models.VoyageRecommendation{Name: "First"}
	h.broadcastRecommendation(sessionID, rec) // fills buffer

	// Second broadcast must not block even though buffer is full.
	done := make(chan struct{})
	go func() {
		h.broadcastRecommendation(sessionID, models.VoyageRecommendation{Name: "Second"})
		close(done)
	}()

	select {
	case <-done:
		// Correct: returned without blocking.
	case <-time.After(200 * time.Millisecond):
		t.Error("broadcastRecommendation blocked on a full channel")
	}
}

// TestRecStreams_NilMap_DoesNotPanic verifies that broadcasting to a Handler
// with an uninitialised recStreams map (e.g. constructed directly in tests)
// does not panic, since map reads with ok-idiom are safe on nil maps.
func TestRecStreams_NilMap_DoesNotPanic(t *testing.T) {
	h := &Handler{} // recStreams is nil
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("broadcastRecommendation panicked on nil map: %v", r)
		}
	}()
	h.broadcastRecommendation("any-session", models.VoyageRecommendation{Name: "X"})
}

func newHandlerForSSETest() *Handler {
	return &Handler{
		recStreams:  make(map[string]chan models.VoyageRecommendation),
		ResearchSem: make(chan struct{}, 10),
	}
}
