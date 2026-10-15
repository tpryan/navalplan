package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"app/internal/store"
)

func (s *Server) newCookie(name, value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Expires:  expires,
		HttpOnly: true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   s != nil && s.Env == "production",
	}
}

func (s *Server) oauthGoogleLogin(w http.ResponseWriter, r *http.Request) {
	oauthState, err := s.generateStateOauthCookie(w)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to generate oauth state", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	u := s.GoogleConfig.AuthCodeURL(oauthState)
	http.Redirect(w, r, u, http.StatusTemporaryRedirect)
}

func (s *Server) oauthGoogleCallback(w http.ResponseWriter, r *http.Request) {
	oauthState, _ := r.Cookie("oauthstate")

	if oauthState == nil || r.FormValue("state") != oauthState.Value {
		slog.ErrorContext(r.Context(), "invalid oauth google state")
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	data, err := s.getUserDataFromGoogle(r.Context(), r.FormValue("code"))
	if err != nil {
		slog.ErrorContext(r.Context(), "getUserDataFromGoogle", "error", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	var gUser struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal(data, &gUser); err != nil {
		slog.ErrorContext(r.Context(), "json unmarshal", "error", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	person, err := s.DB.FindPersonByGoogleID(r.Context(), gUser.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "db find person", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if person == nil {
		invitation, err := s.DB.GetInvitation(r.Context(), gUser.Email)
		if err != nil {
			if err == store.ErrInvitationNotFound {
				slog.InfoContext(r.Context(), "uninvited user attempted to log in", "email", gUser.Email)
				http.Redirect(w, r, "/unauthorized.html", http.StatusTemporaryRedirect)
				return
			}
			slog.ErrorContext(r.Context(), "failed to get invitation", "error", err)
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}

		person, err = s.DB.CreatePerson(r.Context(), gUser.ID, gUser.Email, gUser.Name, &gUser.Picture, invitation.InvitedBy, invitation.IsAdmin)
		if err != nil {
			slog.ErrorContext(r.Context(), "db create person", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Carry a temporary-invite expiration onto the new account.
		if invitation.ExpiresAt != nil {
			if err := s.DB.UpdatePersonExpiration(r.Context(), person.ID, invitation.ExpiresAt); err != nil {
				slog.ErrorContext(r.Context(), "failed to set person expiration", "email", gUser.Email, "error", err)
			} else {
				person.ExpiresAt = invitation.ExpiresAt
			}
		}

		if err := s.DB.DeleteInvitation(r.Context(), gUser.Email); err != nil {
			slog.ErrorContext(r.Context(), "failed to delete invitation", "email", gUser.Email, "error", err)
		}
	}

	// Expired accounts may not log in. Middleware also blocks their API
	// access, but refusing the session here stops the login outright.
	if person.ExpiresAt != nil && person.ExpiresAt.Before(time.Now()) {
		slog.InfoContext(r.Context(), "expired user attempted to log in", "email", gUser.Email)
		http.Redirect(w, r, "/unauthorized.html", http.StatusTemporaryRedirect)
		return
	}

	token, err := generateSessionToken()
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to generate session token", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := s.DB.CreateSession(r.Context(), token, person.ID, expiresAt); err != nil {
		slog.ErrorContext(r.Context(), "db create session", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, s.newCookie("navalplan_session", token, expiresAt))

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) oauthLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("navalplan_session")
	if err == nil {
		if err := s.DB.DeleteSession(r.Context(), c.Value); err != nil {
			slog.WarnContext(r.Context(), "failed to delete session on logout", "error", err)
		}
	}
	http.SetCookie(w, s.newCookie("navalplan_session", "", time.Now().Add(-1*time.Hour)))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) generateStateOauthCookie(w http.ResponseWriter) (string, error) {
	expiration := time.Now().Add(20 * time.Minute)
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}
	state := base64.URLEncoding.EncodeToString(b)
	http.SetCookie(w, s.newCookie("oauthstate", state, expiration))
	return state, nil
}

func generateStateOauthCookie(w http.ResponseWriter) (string, error) {
	var s *Server
	return s.generateStateOauthCookie(w)
}

func (s *Server) getUserDataFromGoogle(ctx context.Context, code string) ([]byte, error) {
	token, err := s.GoogleConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange wrong: %w", err)
	}
	userinfoURL := "https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + url.QueryEscape(token.AccessToken)
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, userinfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build userinfo request: %w", err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed getting user info: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed read response: %w", err)
	}
	return contents, nil
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
