package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	appContext "app/context"
)

// AdminUserView is the UI representation of a user/invite
type AdminUserView struct {
	ID      int64  `json:"id,omitempty"`
	Email   string `json:"email"`
	Name    string `json:"name,omitempty"`
	IsAdmin bool   `json:"is_admin"`
	Invited bool   `json:"invited"`
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
			ID:      p.ID,
			Email:   p.Email,
			Name:    p.Name,
			IsAdmin: p.IsAdmin,
			Invited: false,
		})
	}

	// Invites are small enough to just list, but separate them in response structure
	var inviteView []AdminUserView
	for _, i := range invites {
		inviteView = append(inviteView, AdminUserView{
			Email:   i.Email,
			Invited: true,
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
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request")
		return
	}

	currentUser := appContext.GetPersonFromContext(r.Context())
	if currentUser == nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := h.DB.CreateInvitation(r.Context(), req.Email, currentUser.ID, false); err != nil {
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
