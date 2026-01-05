package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	appcontext "app/context"
	"app/models"

	"github.com/charmbracelet/log"
)

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// EnableSharing generates a public share token for a voyage.
func (h *Handler) EnableSharing(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Check ownership
	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if v.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	token := generateToken()
	if err := h.DB.UpdateVoyageSharing(r.Context(), id, &token, true); err != nil {
		log.Error("Failed to enable sharing", "voyage_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	v, _ = h.DB.GetVoyage(r.Context(), id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// DisableSharing revokes the public share token for a voyage.
func (h *Handler) DisableSharing(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Check ownership
	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if v.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	if err := h.DB.UpdateVoyageSharing(r.Context(), id, nil, false); err != nil {
		log.Error("Failed to disable sharing", "voyage_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	v, _ = h.DB.GetVoyage(r.Context(), id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// GetPublicVoyage retrieves a shared voyage by its token.
func (h *Handler) GetPublicVoyage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	v, err := h.DB.GetVoyageByToken(r.Context(), token)
	if err != nil {
		http.Error(w, "Voyage not found or not shared", http.StatusNotFound)
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
		http.Error(w, "Voyage not found or not shared", http.StatusNotFound)
		return
	}

	stops, err := h.DB.ListStops(r.Context(), v.ID, 0, 0)
	if err != nil {
		log.Error("Failed to list public stops", "voyage_id", v.ID, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stops)
}

// ListVoyages returns all voyages for the authenticated user.
func (h *Handler) ListVoyages(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
		log.Error("Failed to list voyages", "person_id", person.ID, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(voyages)
}

// CreateVoyage creates a new voyage for the authenticated user.
func (h *Handler) CreateVoyage(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var v models.Voyage
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.PersonID = person.ID

	if v.SearchRadius < 0 {
		http.Error(w, "Search radius cannot be negative", http.StatusBadRequest)
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
		http.Error(w, "Invalid search radius unit", http.StatusBadRequest)
		return
	}

	if err := h.DB.CreateVoyage(r.Context(), &v); err != nil {
		log.Error("Failed to create voyage", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if v.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// UpdateVoyage updates an existing voyage.
func (h *Handler) UpdateVoyage(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Check ownership first
	existing, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if existing.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	var v models.Voyage
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v.ID = id
	v.PersonID = person.ID // Ensure PersonID isn't changed/spoofed in body

	if err := h.DB.UpdateVoyage(r.Context(), &v); err != nil {
		log.Error("Failed to update voyage", "voyage_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// DeleteVoyage deletes a voyage by ID.
func (h *Handler) DeleteVoyage(w http.ResponseWriter, r *http.Request) {
	person := appcontext.GetPersonFromContext(r.Context())
	if person == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Check ownership
	v, err := h.DB.GetVoyage(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if v.PersonID != person.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	if err := h.DB.DeleteVoyage(r.Context(), id); err != nil {
		log.Error("Failed to delete voyage", "voyage_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"msg": "Deleted"})
}
