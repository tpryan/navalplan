package server

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	appcontext "app/context"

	"github.com/charmbracelet/log"
)

type rateLimitEntry struct {
	count    int
	lastSeen time.Time
}

var (
	rateLimits = sync.Map{}
)

// rateLimit is a simple in-memory rate limiter.
// It allows 'limit' requests per 'window' duration.
// This is not distributed and resets on server restart.
func (s *Server) rateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Identify user by Session (if auth) or IP
			var key string
			person := appcontext.GetPersonFromContext(r.Context())
			if person != nil {
				key = fmt.Sprintf("user:%d", person.ID)
			} else {
				key = fmt.Sprintf("ip:%s", r.RemoteAddr)
			}

			// Clean/Check
			now := time.Now()

			// Load or Store
			val, loaded := rateLimits.LoadOrStore(key, &rateLimitEntry{count: 1, lastSeen: now})
			entry := val.(*rateLimitEntry)

			if loaded {
				// Check window
				if now.Sub(entry.lastSeen) > window {
					// Reset
					entry.count = 1
					entry.lastSeen = now
				} else {
					entry.count++
				}
			}

			if entry.count > limit {
				log.Warn("Rate limit exceeded", "key", key, "limit", limit)
				http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// secureHeaders sets security-related headers on the response.
func (s *Server) secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains") // TODO: Enable once HTTPS is enforced
		next.ServeHTTP(w, r)
	})
}

// staticCache adds Cache-Control headers to static assets.
func (s *Server) staticCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assets with hashes in their names can be cached for a long time.
		if strings.Contains(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	Writer *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	return g.Writer.Write(b)
}

// gzipMiddleware compresses the response using gzip if the client supports it.
func (s *Server) gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Don't compress small responses or images
		// http.ServeFile might set content type later, so we check extension
		ext := filepath.Ext(r.URL.Path)
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".ico" {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()

		gzw := &gzipResponseWriter{ResponseWriter: w, Writer: gz}
		next.ServeHTTP(gzw, r)
	})
}

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
			log.Warn("requireAuth: navalplan_session cookie not found", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// 2. Validate session from DB
		sessionToken := cookie.Value
		session, err := s.DB.GetSession(r.Context(), sessionToken)
		if err != nil {
			log.Error("requireAuth: DB error getting session", "token", sessionToken, "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if session == nil {
			log.Warn("requireAuth: Session not found or expired", "token", sessionToken)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// 4. Get the Person
		person, err := s.DB.GetPersonByID(r.Context(), session.PersonID)
		if err != nil {
			log.Error("requireAuth: Error getting person", "person_id", session.PersonID, "error", err)
			http.Error(w, "user not found", http.StatusUnauthorized)
			return
		}
		if person == nil {
			log.Warn("requireAuth: Person not found", "person_id", session.PersonID)
			http.Error(w, "user not found", http.StatusUnauthorized)
			return
		}

		// 5. Add to Context and Proceed
		ctx := appcontext.AddPersonToContext(r.Context(), person)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
