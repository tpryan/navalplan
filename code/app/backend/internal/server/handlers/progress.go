package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"app/internal/model"
)

func (h *Handler) ensureProgressChannel(sessionID string, ttl time.Duration) chan model.ProgressEvent {
	h.muProgressStreams.Lock()
	if ch, ok := h.progressStreams[sessionID]; ok {
		h.muProgressStreams.Unlock()
		return ch
	}
	ch := make(chan model.ProgressEvent, 30)
	h.progressStreams[sessionID] = ch
	h.muProgressStreams.Unlock()

	go func(sid string, created chan model.ProgressEvent) {
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
		case ch <- model.ProgressEvent{Stage: stage, Message: message}:
		default:
		}
	}
}

func (h *Handler) StreamProgress(w http.ResponseWriter, r *http.Request) {
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

	ch := h.ensureProgressChannel(sessionID, 30*time.Minute)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			rc.Flush()
		case evt, ok := <-ch:
			if !ok {
				return
			}
			jsonData, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: progress\ndata: %s\n\n", jsonData)
			rc.Flush()
			if evt.Stage == "done" || evt.Stage == "error" || evt.Stage == "error_503" || strings.HasPrefix(evt.Stage, "error") {
				return
			}
		}
	}
}
