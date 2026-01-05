package server

import (
	"net/http"
	"strings"

	appcontext "app/context"
)

func (s *Server) enforceCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow Safe Methods
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Check for Custom Header
		if r.Header.Get("X-Requested-With") != "" {
			next.ServeHTTP(w, r)
			return
		}

		// Check for Content-Type: application/json
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "CSRF Protection: Missing X-Requested-With header or JSON Content-Type", http.StatusForbidden)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Get the session cookie
		cookie, err := r.Cookie("navalplan_session")
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// 2. Validate session from DB
		sessionToken := cookie.Value
		session, err := s.DB.GetSession(r.Context(), sessionToken)
		if err != nil {
			// Log error if needed
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if session == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// 3. Check Expiration (if not done in SQL query)
		// The SQL query already checks expires_at > NOW(), but we can double check or rely on query.
		// session.IsExpired() method does not exist on model yet, relying on SQL.

		// 4. Get the Person
		person, err := s.DB.GetPersonByID(r.Context(), session.PersonID)
		if err != nil {
			http.Error(w, "user not found", http.StatusUnauthorized)
			return
		}

		// 5. Add to Context and Proceed
		ctx := appcontext.AddPersonToContext(r.Context(), person)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
