package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	appcontext "app/context"
	"app/models"
)

// EnableSharing generates a public share token for a voyage.
func (h *Handler) EnableSharing(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	// Check ownership
	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	token, err := h.DB.UpdateVoyageSharing(r.Context(), id, true)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to enable sharing", "voyage_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	v.ShareToken = &token
	v.IsPublic = true
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// DisableSharing revokes the public share token for a voyage.
func (h *Handler) DisableSharing(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	// Check ownership
	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	if _, err := h.DB.UpdateVoyageSharing(r.Context(), id, false); err != nil {
		slog.ErrorContext(r.Context(), "Failed to disable sharing", "voyage_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	v.ShareToken = nil
	v.IsPublic = false
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// GetPublicVoyage retrieves a shared voyage by its token.
func (h *Handler) GetPublicVoyage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	v, err := h.DB.GetVoyageByToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found or not shared")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// GetPublicStops retrieves stops for a shared voyage.
func (h *Handler) GetPublicStops(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	v, err := h.DB.GetVoyageByToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found or not shared")
		return
	}

	stops, err := h.DB.ListStops(r.Context(), v.ID, 0, 0)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list public stops", "voyage_id", v.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stops)
}

// ListVoyages returns all voyages for the authenticated user.
func (h *Handler) ListVoyages(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	voyages, err := h.DB.ListVoyages(r.Context(), person.ID, limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list voyages", "person_id", person.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(voyages)
}

// CreateVoyage creates a new voyage for the authenticated user.
func (h *Handler) CreateVoyage(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var v models.Voyage
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	v.PersonID = person.ID

	if v.SearchRadius < 0 {
		writeError(w, http.StatusBadRequest, "Search radius cannot be negative")
		return
	}
	if v.SearchRadius == 0 {
		v.SearchRadius = 60
	}
	switch v.SearchRadiusUnit {
	case "nm", "km", "mi":
		// ok
	case "":
		v.SearchRadiusUnit = "nm"
	default:
		writeError(w, http.StatusBadRequest, "Invalid search radius unit")
		return
	}

	if err := h.DB.CreateVoyage(r.Context(), &v); err != nil {
		slog.ErrorContext(r.Context(), "Failed to create voyage", "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(v)
}

// GetVoyage retrieves a specific voyage by ID.
func (h *Handler) GetVoyage(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// UpdateVoyage updates an existing voyage.
func (h *Handler) UpdateVoyage(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	// Check ownership first
	existing, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if existing.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	var v models.Voyage
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	v.ID = id
	v.PersonID = person.ID // Ensure PersonID isn't changed/spoofed in body

	if err := h.DB.UpdateVoyage(r.Context(), &v); err != nil {
		slog.ErrorContext(r.Context(), "Failed to update voyage", "voyage_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// DeleteVoyage deletes a voyage by ID.
func (h *Handler) DeleteVoyage(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	// Check ownership
	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	if err := h.DB.DeleteVoyage(r.Context(), id); err != nil {
		slog.ErrorContext(r.Context(), "Failed to delete voyage", "voyage_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Deleted"})
}

// ExtendVoyage lengthens a voyage's timeline to accommodate additional passage days.
// It accepts either an explicit end_date or an additional_days count, updates the voyage,
// then recomputes at-sea passage points and kicks off offshore weather research for them.
func (h *Handler) ExtendVoyage(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	var req struct {
		EndDate        *string `json:"end_date"`
		AdditionalDays *int    `json:"additional_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var newEnd time.Time
	switch {
	case req.EndDate != nil && *req.EndDate != "":
		parsed, perr := time.Parse("2006-01-02", *req.EndDate)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "Invalid end_date; expected YYYY-MM-DD")
			return
		}
		newEnd = parsed
	case req.AdditionalDays != nil:
		if *req.AdditionalDays <= 0 {
			writeError(w, http.StatusBadRequest, "additional_days must be positive")
			return
		}
		base := v.EndDate
		if base == nil {
			base = v.StartDate
		}
		if base == nil {
			writeError(w, http.StatusBadRequest, "Voyage has no dates to extend")
			return
		}
		newEnd = base.AddDate(0, 0, *req.AdditionalDays)
	default:
		writeError(w, http.StatusBadRequest, "Provide end_date or additional_days")
		return
	}

	if v.StartDate != nil && newEnd.Before(*v.StartDate) {
		writeError(w, http.StatusBadRequest, "end_date cannot be before start_date")
		return
	}

	if err := h.DB.UpdateVoyageDates(r.Context(), id, v.StartDate, &newEnd); err != nil {
		slog.ErrorContext(r.Context(), "Failed to extend voyage", "voyage_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	v.EndDate = &newEnd

	// Recompute passage points for any multi-day gaps between landfalls and research them.
	points, err := h.InterpolatePassagePoints(r.Context(), id)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to interpolate passage points", "voyage_id", id, "err", err)
	} else if len(points) > 0 {
		h.researchPassagePoints(points)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"voyage":         v,
		"passage_points": len(points),
	})
}

// UpdateVoyageConfig adjusts a voyage's search radius. Wide ocean transits may use a
// radius up to 300 to reflect regional-scale forecasting.
func (h *Handler) UpdateVoyageConfig(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	var req struct {
		SearchRadius     *int    `json:"search_radius"`
		SearchRadiusUnit *string `json:"search_radius_unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	radius := v.SearchRadius
	if req.SearchRadius != nil {
		radius = *req.SearchRadius
	}
	if radius < 1 || radius > 300 {
		writeError(w, http.StatusBadRequest, "search_radius must be between 1 and 300")
		return
	}

	unit := v.SearchRadiusUnit
	if req.SearchRadiusUnit != nil {
		switch *req.SearchRadiusUnit {
		case "nm", "km", "mi":
			unit = *req.SearchRadiusUnit
		default:
			writeError(w, http.StatusBadRequest, "Invalid search radius unit")
			return
		}
	}
	if unit == "" {
		unit = "nm"
	}

	if err := h.DB.UpdateVoyageConfig(r.Context(), id, radius, unit); err != nil {
		slog.ErrorContext(r.Context(), "Failed to update voyage config", "voyage_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	v.SearchRadius = radius
	v.SearchRadiusUnit = unit

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// GetPilotReport aggregates voyage info, the voyage guide, and all area recommendations.
func (h *Handler) GetPilotReport(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	// 1. Fetch Voyage and check ownership
	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Voyage not found")
		return
	}
	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	// 2. Fetch VoyageGuide (if exists)
	guide, err := h.DB.GetVoyageGuide(r.Context(), id)
	if err != nil {
		// It's okay if it doesn't exist yet
		slog.DebugContext(r.Context(), "Voyage guide not found for pilot report", "voyage_id", id, "err", err)
		guide = nil
	}

	// 3. Fetch Recommendations
	recs, err := h.DB.ListVoyageRecommendations(r.Context(), id)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list recommendations for pilot report", "voyage_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	// 4. Check for Map Snapshot
	var mapURL string
	if mapData, _ := h.DB.GetVoyageMap(r.Context(), id); len(mapData) > 0 {
		mapURL = fmt.Sprintf("/api/v1/voyages/%d/map_image", id)
	}

	report := models.PilotReport{
		Voyage:          v,
		Guide:           guide,
		Recommendations: recs,
		MapURL:          mapURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

// Checkin updates the current GPS position of the voyage owner.
func (h *Handler) Checkin(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	// Check ownership
	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if v.PersonID != person.ID {
		writeError(w, http.StatusForbidden, "Unauthorized")
		return
	}

	var pos struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Location  string  `json:"location_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&pos); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid position data")
		return
	}

	if err := h.DB.UpdateVoyageCheckin(r.Context(), id, pos.Latitude, pos.Longitude, pos.Location); err != nil {
		slog.ErrorContext(r.Context(), "Failed to update check-in", "voyage_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
