package handlers

import (
	"encoding/json"
	"net/http"

	appContext "app/context"
)

func (h *Handler) GetPerson(w http.ResponseWriter, r *http.Request) {
	person := appContext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(person)
}

func (h *Handler) UpdatePerson(w http.ResponseWriter, r *http.Request) {
	person := appContext.GetPersonFromContext(r.Context())
	if person == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if len(req.Name) == 0 || len(req.Name) > 255 {
		writeError(w, http.StatusBadRequest, "Name must be between 1 and 255 characters")
		return
	}

	if err := h.DB.UpdatePersonName(r.Context(), person.ID, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, "Database error")
		return
	}

	person.Name = req.Name
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(person)
}
