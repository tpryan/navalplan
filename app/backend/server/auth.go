package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
)

func (s *Server) oauthGoogleLogin(w http.ResponseWriter, r *http.Request) {
	oauthState := generateStateOauthCookie(w)
	u := s.GoogleConfig.AuthCodeURL(oauthState)
	http.Redirect(w, r, u, http.StatusTemporaryRedirect)
}

func (s *Server) oauthGoogleCallback(w http.ResponseWriter, r *http.Request) {
	oauthState, _ := r.Cookie("oauthstate")

	if r.FormValue("state") != oauthState.Value {
		log.Error("invalid oauth google state")
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	data, err := s.getUserDataFromGoogle(r.FormValue("code"))
	if err != nil {
		log.Error("getUserDataFromGoogle", "error", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	// Unmarshal
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

	// Find or Create Person
	person, err := s.DB.FindPersonByGoogleID(r.Context(), gUser.ID)
	if err != nil {
		log.Error("db find person", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if person == nil {
		person, err = s.DB.CreatePerson(r.Context(), gUser.ID, gUser.Email, gUser.Name, &gUser.Picture)
		if err != nil {
			log.Error("db create person", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	// Create Session
	token := generateSessionToken()
	expiresAt := time.Now().Add(30 * 24 * time.Hour) // 30 days
	if err := s.DB.CreateSession(r.Context(), token, person.ID, expiresAt); err != nil {
		log.Error("db create session", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Set Cookie
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

// Helpers

func generateStateOauthCookie(w http.ResponseWriter) string {
	var expiration = time.Now().Add(20 * time.Minute)
	b := make([]byte, 16)
	rand.Read(b)
	state := base64.URLEncoding.EncodeToString(b)
	cookie := http.Cookie{Name: "oauthstate", Value: state, Expires: expiration, HttpOnly: true}
	http.SetCookie(w, &cookie)
	return state
}

func (s *Server) getUserDataFromGoogle(code string) ([]byte, error) {
	token, err := s.GoogleConfig.Exchange(context.Background(), code)
	if err != nil {
		return nil, fmt.Errorf("code exchange wrong: %s", err.Error())
	}
	response, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
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

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}
