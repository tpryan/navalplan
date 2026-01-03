package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	appcontext "app/context"
	"app/models"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) ListStops(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}

	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	stops, err := h.DB.ListStops(r.Context(), voyageID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stops)
}

func (h *Handler) CreateStop(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}

	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	var s models.Stop
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.VoyageID = voyageID

	if err := h.DB.CreateStop(r.Context(), &s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) UpdateStop(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch stop to get VoyageID
	existingStop, err := h.DB.GetStop(r.Context(), id)
	if err != nil {
		http.Error(w, "Stop not found", http.StatusNotFound)
		return
	}

	// Check Voyage ownership
	voyage, err := h.DB.GetVoyage(r.Context(), existingStop.VoyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	var s models.Stop
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.ID = id
	// Ensure VoyageID is preserved/correct
	s.VoyageID = existingStop.VoyageID

	if err := h.DB.UpdateStop(r.Context(), &s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) DeleteStop(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch stop to get VoyageID
	existingStop, err := h.DB.GetStop(r.Context(), id)
	if err != nil {
		http.Error(w, "Stop not found", http.StatusNotFound)
		return
	}

	// Check Voyage ownership
	voyage, err := h.DB.GetVoyage(r.Context(), existingStop.VoyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}
	if voyage.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	if err := h.DB.DeleteStop(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Deleted"})
}
