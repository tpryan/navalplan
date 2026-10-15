package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"app/internal/model"
)

func validateStop(s *model.Stop) error {
	switch s.StopType {
	case model.StopTypeLandfall, model.StopTypePassagePoint:
	case "":
		s.StopType = model.StopTypeLandfall
	default:
		return errors.New("Invalid stop type")
	}

	if s.Latitude < -90 || s.Latitude > 90 {
		return errors.New("Invalid latitude")
	}
	if s.Longitude < -180 || s.Longitude > 180 {
		return errors.New("Invalid longitude")
	}
	if s.SearchRadius < 0 {
		return errors.New("Search radius cannot be negative")
	}
	if s.SearchRadius == 0 {
		s.SearchRadius = 20
	}
	switch s.SearchRadiusUnit {
	case "nm", "km", "mi":
	case "":
		s.SearchRadiusUnit = "nm"
	default:
		return errors.New("Invalid search radius unit")
	}
	return nil
}

func (h *Handler) ListStops(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := voyage.ID

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
		slog.ErrorContext(r.Context(), "Failed to list stops", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stops)
}

func (h *Handler) CreateStop(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := voyage.ID

	var s model.Stop
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.VoyageID = voyageID

	if err := validateStop(&s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.DB.CreateStop(r.Context(), &s); err != nil {
		slog.ErrorContext(r.Context(), "Failed to create stop", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) UpdateStop(w http.ResponseWriter, r *http.Request) {
	existingStop, _, _, ok := h.authorizedStop(w, r)
	if !ok {
		return
	}
	id := existingStop.ID

	var s model.Stop
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.ID = id
	s.VoyageID = existingStop.VoyageID

	if err := validateStop(&s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.DB.UpdateStop(r.Context(), &s); err != nil {
		slog.ErrorContext(r.Context(), "Failed to update stop", "stop_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func (h *Handler) DeleteStop(w http.ResponseWriter, r *http.Request) {
	existingStop, _, _, ok := h.authorizedStop(w, r)
	if !ok {
		return
	}
	id := existingStop.ID

	if err := h.DB.DeleteStop(r.Context(), id); err != nil {
		slog.ErrorContext(r.Context(), "Failed to delete stop", "stop_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Deleted"})
}
