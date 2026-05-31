package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	appcontext "app/context"
	"app/models"
)

func (h *Handler) ListStops(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}

	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
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
		slog.ErrorContext(r.Context(), "Failed to list stops", "voyage_id", voyageID, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stops)
}

func (h *Handler) CreateStop(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Voyage ID")
		return
	}

	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}

	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	var s models.Stop
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.VoyageID = voyageID

	switch s.StopType {
	case models.StopTypeLandfall, models.StopTypePassagePoint:
		// ok
	case "":
		s.StopType = models.StopTypeLandfall
	default:
		writeError(w, http.StatusBadRequest, "Invalid stop type")
		return
	}

	if s.Latitude < -90 || s.Latitude > 90 {
		writeError(w, http.StatusBadRequest, "Invalid latitude")
		return
	}
	if s.Longitude < -180 || s.Longitude > 180 {
		writeError(w, http.StatusBadRequest, "Invalid longitude")
		return
	}
	if s.SearchRadius < 0 {
		writeError(w, http.StatusBadRequest, "Search radius cannot be negative")
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
		writeError(w, http.StatusBadRequest, "Invalid search radius unit")
		return
	}

	if err := h.DB.CreateStop(r.Context(), &s); err != nil {
		slog.ErrorContext(r.Context(), "Failed to create stop", "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
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
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Fetch stop to get VoyageID
	existingStop, err := h.DB.GetStop(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Stop not found")
		return
	}

	// Check Voyage ownership
	voyage, err := h.DB.GetVoyage(r.Context(), existingStop.VoyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	var s models.Stop
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.ID = id
	// Ensure VoyageID is preserved/correct
	s.VoyageID = existingStop.VoyageID

	if err := h.DB.UpdateStop(r.Context(), &s); err != nil {
		slog.ErrorContext(r.Context(), "Failed to update stop", "stop_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) DeleteStop(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Fetch stop to get VoyageID
	existingStop, err := h.DB.GetStop(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Stop not found")
		return
	}

	// Check Voyage ownership
	voyage, err := h.DB.GetVoyage(r.Context(), existingStop.VoyageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if voyage.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	if err := h.DB.DeleteStop(r.Context(), id); err != nil {
		slog.ErrorContext(r.Context(), "Failed to delete stop", "stop_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Deleted"})
}
