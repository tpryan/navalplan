package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) TriggerResearch(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Stop ID", http.StatusBadRequest)
		return
	}

	// TODO: Call Researcher Microservice asynchronously
	// For now, we just return 202 Accepted
	
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Research started", "stop_id": strconv.FormatInt(stopID, 10)})
}

func (h *Handler) GetBriefing(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	stopID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Stop ID", http.StatusBadRequest)
		return
	}

	briefing, err := h.DB.GetBriefing(stopID)
	if err != nil {
		http.Error(w, "Briefing not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(briefing)
}
