package handlers

import (
	"encoding/json"
	"net/http"

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
	people, _ := h.DB.ListPeople(r.Context())
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
	for _, i := range invites {
		view = append(view, AdminUserView{
			Email:   i.Email,
			Invited: true,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"users": view})
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
