package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateSessionToken(t *testing.T) {
	token, err := generateSessionToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(token) == 0 {
		t.Fatal("expected non-empty token")
	}
	if len(token) < 40 {
		t.Errorf("token too short, got %d chars: %q", len(token), token)
	}

	token2, err := generateSessionToken()
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if token == token2 {
		t.Error("expected different tokens on successive calls")
	}
}

func TestGenerateStateOauthCookie(t *testing.T) {
	w := httptest.NewRecorder()

	state, err := generateStateOauthCookie(w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state) == 0 {
		t.Fatal("expected non-empty state")
	}

	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "oauthstate" {
			found = true
			if c.Value != state {
				t.Errorf("cookie value %q does not match returned state %q", c.Value, state)
			}
			if !c.HttpOnly {
				t.Error("expected HttpOnly cookie")
			}
		}
	}
	if !found {
		t.Error("oauthstate cookie not set")
	}
}

func TestGenerateStateOauthCookieUniqueness(t *testing.T) {
	states := make(map[string]bool)
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		state, err := generateStateOauthCookie(w)
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
		if states[state] {
			t.Errorf("duplicate state generated: %q", state)
		}
		states[state] = true
	}
}

func TestGenerateSessionTokenNoPadding(t *testing.T) {
	token, err := generateSessionToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.ContainsAny(token, "+/") {
		t.Errorf("token contains standard base64 characters that may be unsafe in cookies: %q", token)
	}
}
