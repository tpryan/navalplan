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

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

var (
	timeWarn       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
	timeUrgentWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
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
		// 0. Check System API Key (for Cloud Scheduler / Internal tasks)
		if s.SystemAPIKey != "" {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "Bearer "+s.SystemAPIKey {
				// Bypass session check
				next.ServeHTTP(w, r)
				return
			}
		}

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

		// 4. Get the Person
		person, err := s.DB.GetPersonByID(r.Context(), session.PersonID)
		if err != nil {
			http.Error(w, "user not found", http.StatusUnauthorized)
			return
		}
		if person == nil {
			http.Error(w, "user not found", http.StatusUnauthorized)
			return
		}

		// 5. Add to Context and Proceed
		ctx := appcontext.AddPersonToContext(r.Context(), person)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin checks if the authenticated user is an admin.
// It assumes requireAuth has already run and populated the context.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		person := appcontext.GetPersonFromContext(r.Context())
		if person == nil || !person.IsAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// responseWriter is a wrapper to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (s *Server) traceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceHeader := r.Header.Get("X-Cloud-Trace-Context")
		traceParts := strings.Split(traceHeader, "/")
		if len(traceParts) > 0 && len(traceParts[0]) > 0 {
			traceID := traceParts[0]
			trace := fmt.Sprintf("projects/%s/traces/%s", s.Project, traceID)
			ctx := appcontext.AddTraceToContext(r.Context(), trace)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Request Logging wrapper
		// We need to wrap ResponseWriter to capture status code
		ww := &responseWriter{w, http.StatusOK}

		next.ServeHTTP(ww, r)

		if !strings.Contains(r.URL.Path, "/.well-known") {

			timesince := time.Since(start)
			str := timesince.String()

			level := log.DebugLevel
			severity := "INFO"

			switch {
			case ww.statusCode > 400:
				level = log.WarnLevel
			case ww.statusCode > 500:
				level = log.ErrorLevel
			default:
				level = log.InfoLevel
			}

			switch s.Env {
			case "production":
				// Include the trace field in the log entry
				trace := appcontext.GetTraceFromContext(r.Context())
				log.Log(level, "Request handled",
					"severity", severity,
					"logging.googleapis.com/trace", trace,
					"method", r.Method,
					"path", r.URL.Path,
					"duration", timesince.String(),
				)
			default:
				switch {
				case timesince > time.Second*2:
					str = timeUrgentWarn.Render(str)
				case timesince > time.Millisecond*100:
					str = timeWarn.Render(str)
				}
				log.Log(level, fmt.Sprintf("%s %s %s %d %s", r.Method, r.URL.Path, r.RemoteAddr, ww.statusCode, str))
			}
		}
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowedOrigins := map[string]bool{
			s.BaseURL:               true,
			"http://localhost:5173": true, // Vite default
		}

		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Error("Panic recovered", "error", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
