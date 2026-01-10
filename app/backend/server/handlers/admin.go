package handlers

import (
	"encoding/json"
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

	people, _ := h.DB.ListPeople(r.Context(), limit, offset)
	totalPeople, _ := h.DB.CountPeople(r.Context())
	invites, _ := h.DB.ListInvitations(r.Context())

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
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	currentUser := appContext.GetPersonFromContext(r.Context())
	if currentUser == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.DB.CreateInvitation(r.Context(), req.Email, currentUser.ID); err != nil {
		http.Error(w, "Failed to create invitation", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "invited"}`))
}

func (h *Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	h.DB.DeleteInvitation(r.Context(), email)
	w.WriteHeader(http.StatusOK)
}
