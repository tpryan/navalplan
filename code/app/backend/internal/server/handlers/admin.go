package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

type AdminUserView struct {
	ID        int64      `json:"id,omitempty"`
	Email     string     `json:"email"`
	Name      string     `json:"name,omitempty"`
	IsAdmin   bool       `json:"is_admin"`
	Invited   bool       `json:"invited"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// parseExpiry interprets an optional expiration value from an admin request.
// An empty string clears/omits the expiration (permanent). It accepts an
// RFC3339 timestamp or a plain "YYYY-MM-DD" date (interpreted as end of that
// UTC day).
func parseExpiry(s string) (*time.Time, bool) {
	if s == "" {
		return nil, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t, true
	}
	if d, err := time.Parse("2006-01-02", s); err == nil {
		eod := time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, time.UTC)
		return &eod, true
	}
	return nil, false
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page := 1
	limit := 20

	if p := r.URL.Query().Get("page"); p != "" {
		if val, err := strconv.Atoi(p); err == nil && val > 0 {
			page = val
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}

	offset := (page - 1) * limit

	people, err := h.DB.ListPeople(r.Context(), limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list people", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	totalPeople, err := h.DB.CountPeople(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to count people", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	invites, err := h.DB.ListInvitations(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list invitations", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	var view []AdminUserView

	for _, p := range people {
		view = append(view, AdminUserView{
			ID:        p.ID,
			Email:     p.Email,
			Name:      p.Name,
			IsAdmin:   p.IsAdmin,
			Invited:   false,
			ExpiresAt: p.ExpiresAt,
		})
	}

	var inviteView []AdminUserView
	for _, i := range invites {
		inviteView = append(inviteView, AdminUserView{
			Email:     i.Email,
			Invited:   true,
			ExpiresAt: i.ExpiresAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"users": map[string]interface{}{
			"data":  view,
			"total": totalPeople,
			"page":  page,
			"limit": limit,
		},
		"invites": inviteView,
	})
}

func (h *Handler) InviteUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email     string `json:"email"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request")
		return
	}

	currentUser := GetPersonFromContext(r.Context())
	if currentUser == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	expiresAt, ok := parseExpiry(req.ExpiresAt)
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid expires_at (use RFC3339 or YYYY-MM-DD)")
		return
	}

	if err := h.DB.CreateInvitation(r.Context(), req.Email, currentUser.ID, false, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create invitation")
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "invited"}`))
}

func (h *Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if err := h.DB.DeleteInvitation(r.Context(), email); err != nil {
		slog.ErrorContext(r.Context(), "Failed to delete invitation", "email", email, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// SetUserExpiration sets or clears an existing user's account expiration. An
// empty/omitted expires_at makes the account permanent again.
func (h *Handler) SetUserExpiration(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid user id")
		return
	}

	var req struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request")
		return
	}

	expiresAt, ok := parseExpiry(req.ExpiresAt)
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid expires_at (use RFC3339 or YYYY-MM-DD)")
		return
	}

	if err := h.DB.UpdatePersonExpiration(r.Context(), id, expiresAt); err != nil {
		slog.ErrorContext(r.Context(), "Failed to update user expiration", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "updated"}`))
}
