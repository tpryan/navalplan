package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"app/models"
)

// ensureProgressChannel returns the existing buffered channel for sessionID, or creates
// and registers a new one. Call this in trigger handlers BEFORE spawning the research
// goroutine so that early broadcastProgress calls are buffered until the EventSource
// client connects.  A cleanup goroutine reclaims the channel after ttl if no SSE
// subscriber ever claims it.
func (h *Handler) ensureProgressChannel(sessionID string, ttl time.Duration) chan models.ProgressEvent {
	h.muProgressStreams.Lock()
	if ch, ok := h.progressStreams[sessionID]; ok {
		h.muProgressStreams.Unlock()
		return ch
	}
	ch := make(chan models.ProgressEvent, 30)
	h.progressStreams[sessionID] = ch
	h.muProgressStreams.Unlock()

	// Orphan cleanup: if no SSE client claims the channel within ttl, delete it.
	go func(sid string, created chan models.ProgressEvent) {
		time.Sleep(ttl)
		h.muProgressStreams.Lock()
		if existing, ok := h.progressStreams[sid]; ok && existing == created {
			delete(h.progressStreams, sid)
		}
		h.muProgressStreams.Unlock()
	}(sessionID, ch)

	return ch
}

func (h *Handler) broadcastProgress(sessionID, stage, message string) {
	h.muProgressStreams.RLock()
	defer h.muProgressStreams.RUnlock()
	if ch, ok := h.progressStreams[sessionID]; ok {
		select {
		case ch <- models.ProgressEvent{Stage: stage, Message: message}:
		default:
		}
	}
}

// StreamProgress provides an SSE endpoint for real-time research progress updates.
func (h *Handler) StreamProgress(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "Missing session_id")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	rc := http.NewResponseController(w)

	// Use existing pre-registered channel (created by trigger handler before the
	// research goroutine started) so buffered events are not lost.
	ch := h.ensureProgressChannel(sessionID, 30*time.Minute)

	var once sync.Once
	closeCh := func() { once.Do(func() { close(ch) }) }

	defer func() {
		h.muProgressStreams.Lock()
		delete(h.progressStreams, sessionID)
		closeCh()
		h.muProgressStreams.Unlock()
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			jsonData, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: progress\ndata: %s\n\n", jsonData)
			rc.Flush()
		}
	}
}
