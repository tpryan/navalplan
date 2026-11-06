package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"app/internal/model"
)

func isSingleDayTrip(v *model.Voyage) bool {
	if v == nil || v.StartDate == nil || v.EndDate == nil {
		return false
	}
	return v.StartDate.UTC().Format("2006-01-02") == v.EndDate.UTC().Format("2006-01-02")
}

func applyDayAutoDefaults(ctx context.Context, db DBStore, v *model.Voyage, d *model.VoyageDay) {
	isDayTrip := isSingleDayTrip(v) || d.DayType == model.DayTypeDayTrip || d.DayType == model.DayTypeLayDay

	hasStart := d.StartLatitude != 0 || d.StartLongitude != 0 || d.StartLocationName != ""
	hasEnd := d.EndLatitude != 0 || d.EndLongitude != 0 || d.EndLocationName != ""

	// 1. If not a day trip and start is unset, try auto-populating from previous day's end
	if !hasStart && db != nil && !d.TargetDate.IsZero() {
		if prev, err := db.GetPreviousDay(ctx, d.VoyageID, d.TargetDate); err == nil && prev != nil {
			if prev.EndLatitude != 0 || prev.EndLongitude != 0 || prev.EndLocationName != "" {
				d.StartLatitude = prev.EndLatitude
				d.StartLongitude = prev.EndLongitude
				d.StartLocationName = prev.EndLocationName
				d.StartPreciseLocation = prev.EndPreciseLocation
				hasStart = true
			}
		}
	}

	// 2. Day trips / lay days or single-point submissions mirror Start <-> End
	if isDayTrip {
		if d.DayType == "" && isSingleDayTrip(v) {
			d.DayType = model.DayTypeDayTrip
		}
		if hasStart && !hasEnd {
			d.EndLatitude = d.StartLatitude
			d.EndLongitude = d.StartLongitude
			d.EndLocationName = d.StartLocationName
			d.EndPreciseLocation = d.StartPreciseLocation
			hasEnd = true
		} else if hasEnd && !hasStart {
			d.StartLatitude = d.EndLatitude
			d.StartLongitude = d.EndLongitude
			d.StartLocationName = d.EndLocationName
			d.StartPreciseLocation = d.EndPreciseLocation
			hasStart = true
		}
	} else {
		if hasStart && !hasEnd {
			d.EndLatitude = d.StartLatitude
			d.EndLongitude = d.StartLongitude
			d.EndLocationName = d.StartLocationName
			d.EndPreciseLocation = d.StartPreciseLocation
		} else if hasEnd && !hasStart {
			d.StartLatitude = d.EndLatitude
			d.StartLongitude = d.EndLongitude
			d.StartLocationName = d.EndLocationName
			d.StartPreciseLocation = d.EndPreciseLocation
		}
	}
}

func validateDay(d *model.VoyageDay) error {
	switch d.DayType {
	case model.DayTypeLandfall, model.DayTypePassagePoint, model.DayTypeLayDay, model.DayTypeDayTrip:
	case "":
		d.DayType = model.DayTypeLandfall
	default:
		return errors.New("Invalid day type")
	}
	if d.StartLatitude < -90 || d.StartLatitude > 90 || d.EndLatitude < -90 || d.EndLatitude > 90 {
		return errors.New("Invalid latitude")
	}
	if d.StartLongitude < -180 || d.StartLongitude > 180 || d.EndLongitude < -180 || d.EndLongitude > 180 {
		return errors.New("Invalid longitude")
	}
	if d.SearchRadius < 0 {
		return errors.New("Search radius cannot be negative")
	}
	if d.SearchRadius == 0 {
		d.SearchRadius = 20
	}
	switch d.SearchRadiusUnit {
	case "nm", "km", "mi":
	case "":
		d.SearchRadiusUnit = "nm"
	default:
		return errors.New("Invalid search radius unit")
	}
	return nil
}

func (h *Handler) ListDays(w http.ResponseWriter, r *http.Request) {
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

	days, err := h.DB.ListDays(r.Context(), voyageID, limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list days", "voyage_id", voyageID, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(days)
}

func (h *Handler) CreateDay(w http.ResponseWriter, r *http.Request) {
	voyage, _, ok := h.authorizedVoyage(w, r)
	if !ok {
		return
	}
	voyageID := voyage.ID

	var d model.VoyageDay
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d.VoyageID = voyageID

	applyDayAutoDefaults(r.Context(), h.DB, voyage, &d)

	if err := validateDay(&d); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.DB.CreateDay(r.Context(), &d); err != nil {
		slog.ErrorContext(r.Context(), "Failed to create day", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(d)
}

func (h *Handler) UpdateDay(w http.ResponseWriter, r *http.Request) {
	existingDay, voyage, _, ok := h.authorizedDay(w, r)
	if !ok {
		return
	}
	id := existingDay.ID

	var d model.VoyageDay
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d.ID = id
	d.VoyageID = existingDay.VoyageID
	if d.TargetDate.IsZero() {
		d.TargetDate = existingDay.TargetDate
	}

	applyDayAutoDefaults(r.Context(), h.DB, voyage, &d)

	if err := validateDay(&d); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.DB.UpdateDay(r.Context(), &d); err != nil {
		slog.ErrorContext(r.Context(), "Failed to update day", "day_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d)
}

func (h *Handler) DeleteDay(w http.ResponseWriter, r *http.Request) {
	existingDay, _, _, ok := h.authorizedDay(w, r)
	if !ok {
		return
	}
	id := existingDay.ID

	if err := h.DB.DeleteDay(r.Context(), id); err != nil {
		slog.ErrorContext(r.Context(), "Failed to delete day", "day_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Deleted"})
}
