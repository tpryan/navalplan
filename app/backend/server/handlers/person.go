package handlers

import (
	"encoding/json"
	"net/http"

	appContext "app/context"
)

func (h *Handler) GetPerson(w http.ResponseWriter, r *http.Request) {
	person := appContext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(person)
}

func (h *Handler) UpdatePerson(w http.ResponseWriter, r *http.Request) {
	person := appContext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdatePersonName(person.ID, req.Name); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	person.Name = req.Name
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(person)
}
