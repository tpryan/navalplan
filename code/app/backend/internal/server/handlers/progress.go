package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"app/internal/model"
)

type eventHub[T any] struct {
	mu      sync.RWMutex
	streams map[string]chan T
	bufSize int
}

func newEventHub[T any](bufSize int) eventHub[T] {
	return eventHub[T]{
		streams: make(map[string]chan T),
		bufSize: bufSize,
	}
}

func (hub *eventHub[T]) ensure(sessionID string, ttl time.Duration) chan T {
	hub.mu.Lock()
	if hub.streams == nil {
		hub.streams = make(map[string]chan T)
	}
	if ch, ok := hub.streams[sessionID]; ok {
		hub.mu.Unlock()
		return ch
	}
	bufSize := hub.bufSize
	if bufSize <= 0 {
		bufSize = 30
	}
	ch := make(chan T, bufSize)
	hub.streams[sessionID] = ch
	hub.mu.Unlock()

	go func(sid string, created chan T) {
		time.Sleep(ttl)
		hub.mu.Lock()
		if existing, ok := hub.streams[sid]; ok && existing == created {
			delete(hub.streams, sid)
		}
		hub.mu.Unlock()
	}(sessionID, ch)

	return ch
}

func (hub *eventHub[T]) broadcast(sessionID string, evt T) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	if ch, ok := hub.streams[sessionID]; ok {
		select {
		case ch <- evt:
		default:
		}
	}
}

func (hub *eventHub[T]) close(sessionID string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if ch, ok := hub.streams[sessionID]; ok {
		close(ch)
		delete(hub.streams, sessionID)
	}
}

func streamSSE[T any](w http.ResponseWriter, r *http.Request, hub *eventHub[T], eventName string, ttl time.Duration, isDone func(T) bool) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "Missing session_id")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	rc := http.NewResponseController(w)
	fmt.Fprintf(w, ": connected\n\n")
	_ = rc.Flush()

	ch := hub.ensure(sessionID, ttl)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			_ = rc.Flush()
		case evt, ok := <-ch:
			if !ok {
				return
			}
			jsonData, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, jsonData)
			_ = rc.Flush()
			if isDone != nil && isDone(evt) {
				return
			}
		}
	}
}

func (h *Handler) ensureProgressChannel(sessionID string, ttl time.Duration) chan model.ProgressEvent {
	return h.progressHub.ensure(sessionID, ttl)
}

func (h *Handler) broadcastProgress(sessionID, stage, message string) {
	h.progressHub.broadcast(sessionID, model.ProgressEvent{Stage: stage, Message: message})
}

func (h *Handler) StreamProgress(w http.ResponseWriter, r *http.Request) {
	streamSSE(w, r, &h.progressHub, "progress", 30*time.Minute, func(evt model.ProgressEvent) bool {
		return evt.Stage == "done" || strings.HasPrefix(evt.Stage, "error")
	})
}
