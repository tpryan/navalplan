package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"crypto/rand"
	"encoding/hex"

	"app/models"

	"github.com/go-chi/chi/v5"
)

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handler) EnableSharing(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	token := generateToken()
	if err := h.DB.UpdateVoyageSharing(id, &token, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	v, _ := h.DB.GetVoyage(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DisableSharing(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateVoyageSharing(id, nil, false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	v, _ := h.DB.GetVoyage(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) GetPublicVoyage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	v, err := h.DB.GetVoyageByToken(token)
	if err != nil {
		http.Error(w, "Voyage not found or not shared", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) GetPublicStops(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	v, err := h.DB.GetVoyageByToken(token)
	if err != nil {
		http.Error(w, "Voyage not found or not shared", http.StatusNotFound)
		return
	}

	stops, err := h.DB.ListStops(v.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stops)
}

func (h *Handler) ListVoyages(w http.ResponseWriter, r *http.Request) {
	// TODO: Get userID from context/session
	userID := int64(1)

	voyages, err := h.DB.ListVoyages(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(voyages)
}

func (h *Handler) CreateVoyage(w http.ResponseWriter, r *http.Request) {
	var v models.Voyage
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// TODO: Get userID from context
	v.UserID = 1

	if v.SearchRadius == 0 {
		v.SearchRadius = 60
	}
	if v.SearchRadiusUnit == "" {
		v.SearchRadiusUnit = "nm"
	}

	if err := h.DB.CreateVoyage(&v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) GetVoyage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	v, err := h.DB.GetVoyage(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) UpdateVoyage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var v models.Voyage
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v.ID = id

	if err := h.DB.UpdateVoyage(&v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteVoyage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := h.DB.DeleteVoyage(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Deleted"})
}