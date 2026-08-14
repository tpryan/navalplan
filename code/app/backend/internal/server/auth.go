package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"app/internal/store"

	"github.com/charmbracelet/log"
)

func (s *Server) oauthGoogleLogin(w http.ResponseWriter, r *http.Request) {
	oauthState, err := generateStateOauthCookie(w)
	if err != nil {
		log.Error("failed to generate oauth state", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	u := s.GoogleConfig.AuthCodeURL(oauthState)
	http.Redirect(w, r, u, http.StatusTemporaryRedirect)
}

func (s *Server) oauthGoogleCallback(w http.ResponseWriter, r *http.Request) {
	oauthState, _ := r.Cookie("oauthstate")

	if oauthState == nil || r.FormValue("state") != oauthState.Value {
		log.Error("invalid oauth google state")
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	data, err := s.getUserDataFromGoogle(r.Context(), r.FormValue("code"))
	if err != nil {
		log.Error("getUserDataFromGoogle", "error", err)
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
		log.Error("json unmarshal", "error", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	person, err := s.DB.FindPersonByGoogleID(r.Context(), gUser.ID)
	if err != nil {
		log.Error("db find person", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if person == nil {
		invitation, err := s.DB.GetInvitation(r.Context(), gUser.Email)
		if err != nil {
			if err == store.ErrInvitationNotFound {
				log.Info("uninvited user attempted to log in", "email", gUser.Email)
				http.Redirect(w, r, "/unauthorized.html", http.StatusTemporaryRedirect)
				return
			}
			log.Error("failed to get invitation", "error", err)
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}

		person, err = s.DB.CreatePerson(r.Context(), gUser.ID, gUser.Email, gUser.Name, &gUser.Picture, invitation.InvitedBy, invitation.IsAdmin)
		if err != nil {
			log.Error("db create person", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		if err := s.DB.DeleteInvitation(r.Context(), gUser.Email); err != nil {
			log.Error("failed to delete invitation", "email", gUser.Email, "error", err)
		}
	}

	token, err := generateSessionToken()
	if err != nil {
		log.Error("failed to generate session token", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := s.DB.CreateSession(r.Context(), token, person.ID, expiresAt); err != nil {
		log.Error("db create session", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "navalplan_session",
		Value:    token,
		Expires:  expiresAt,
		HttpOnly: true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   s.Env == "production",
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) oauthLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("navalplan_session")
	if err == nil {
		s.DB.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "navalplan_session",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
		Path:     "/",
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func generateStateOauthCookie(w http.ResponseWriter) (string, error) {
	expiration := time.Now().Add(20 * time.Minute)
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}
	state := base64.URLEncoding.EncodeToString(b)
	cookie := http.Cookie{Name: "oauthstate", Value: state, Expires: expiration, HttpOnly: true}
	http.SetCookie(w, &cookie)
	return state, nil
}

func (s *Server) getUserDataFromGoogle(ctx context.Context, code string) ([]byte, error) {
	token, err := s.GoogleConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange wrong: %s", err.Error())
	}
	userinfoURL := "https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + url.QueryEscape(token.AccessToken)
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, userinfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build userinfo request: %s", err.Error())
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed getting user info: %s", err.Error())
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed read response: %s", err.Error())
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
