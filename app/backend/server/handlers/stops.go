package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	appcontext "app/context"
	"app/models"

	"github.com/charmbracelet/log"
)

func (h *Handler) ListStops(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
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

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 50
	}
	offset := (page - 1) * limit

	stops, err := h.DB.ListStops(r.Context(), voyageID, limit, offset)
	if err != nil {
		log.Error("Failed to list stops", "voyage_id", voyageID, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stops)
}

func (h *Handler) CreateStop(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
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

	if s.Latitude < -90 || s.Latitude > 90 {
		http.Error(w, "Invalid latitude", http.StatusBadRequest)
		return
	}
	if s.Longitude < -180 || s.Longitude > 180 {
		http.Error(w, "Invalid longitude", http.StatusBadRequest)
		return
	}
	if s.SearchRadius < 0 {
		http.Error(w, "Search radius cannot be negative", http.StatusBadRequest)
		return
	}
	if s.SearchRadius == 0 {
		s.SearchRadius = 60
	}
	switch s.SearchRadiusUnit {
	case "nm", "km", "mi":
		// ok
	case "":
		s.SearchRadiusUnit = "nm"
	default:
		http.Error(w, "Invalid search radius unit", http.StatusBadRequest)
		return
	}

	if err := h.DB.CreateStop(r.Context(), &s); err != nil {
		log.Error("Failed to create stop", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) UpdateStop(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
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
		log.Error("Failed to update stop", "stop_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) DeleteStop(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
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
		log.Error("Failed to delete stop", "stop_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Deleted"})
}
