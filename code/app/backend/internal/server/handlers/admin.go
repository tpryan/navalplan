package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"app/internal/model"
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

	// view selects which set to return:
	//   "pending" -> outstanding invitations (no provisioned accounts)
	//   "expired" -> provisioned accounts past their expiration
	//   default   -> active provisioned accounts
	viewParam := r.URL.Query().Get("view")

	var (
		people      []model.Person
		totalPeople int
		invites     []model.Invitation
		err         error
	)

	switch viewParam {
	case "pending":
		invites, err = h.DB.ListInvitations(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to list invitations", "error", err)
			writeError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
	default:
		expired := viewParam == "expired"
		people, err = h.DB.ListPeople(r.Context(), expired, limit, offset)
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to list people", "error", err)
			writeError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		totalPeople, err = h.DB.CountPeople(r.Context(), expired)
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to count people", "error", err)
			writeError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
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

// splitEmails parses a comma- or newline-separated list into a de-duplicated,
// trimmed, lower-cased slice of email addresses, preserving input order.
func splitEmails(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '\n', '\r', '\t', ';', ' ':
			return true
		}
		return false
	})
	seen := make(map[string]struct{}, len(fields))
	var out []string
	for _, f := range fields {
		e := strings.ToLower(strings.TrimSpace(f))
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// BulkInviteUsers invites a comma- or newline-separated list of users, applying
// a single optional expiration to all of them.
func (h *Handler) BulkInviteUsers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Emails    string `json:"emails"`
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

	emails := splitEmails(req.Emails)
	if len(emails) == 0 {
		writeError(w, http.StatusBadRequest, "No valid email addresses provided")
		return
	}

	invited := make([]string, 0, len(emails))
	failed := make([]string, 0)
	for _, email := range emails {
		if err := h.DB.CreateInvitation(r.Context(), email, currentUser.ID, false, expiresAt); err != nil {
			slog.ErrorContext(r.Context(), "Failed to create bulk invitation", "email", email, "error", err)
			failed = append(failed, email)
			continue
		}
		invited = append(invited, email)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"invited": invited,
		"failed":  failed,
	})
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
