package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"app/config"
	"app/datastore"
	"app/server/handlers"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var timeWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
var timeUrgentWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))

// Server holds the application dependencies and router.
type Server struct {
	Mux          *http.ServeMux
	DB           datastore.Store
	GoogleConfig *oauth2.Config
	Env          string
	BaseURL      string
	Handler      *handlers.Handler
	SystemAPIKey string
}

// New initializes a new Server with the provided database and configuration.
func New(db datastore.Store, cfg *config.Config) (*Server, error) {
	log.SetOutput(os.Stderr)
	log.SetPrefix("backend")

	if len(cfg.GoogleClientID) > 0 {
		prefix := ""
		if len(cfg.GoogleClientID) >= 5 {
			prefix = cfg.GoogleClientID[:5]
		} else {
			prefix = cfg.GoogleClientID
		}
		log.Infof("Initializing server with Google Client ID prefix: %s... (total length: %d)", prefix, len(cfg.GoogleClientID))
	} else {
		log.Warn("Initializing server with EMPTY Google Client ID!")
	}

	h := handlers.New(db, cfg.ContentDir, cfg.NavalPlanAgentURL)

	s := &Server{
		Mux:          http.NewServeMux(),
		DB:           db,
		Env:          cfg.Env,
		BaseURL:      cfg.BaseURL,
		Handler:      h,
		SystemAPIKey: cfg.SystemAPIKey,
		GoogleConfig: &oauth2.Config{
			RedirectURL:  cfg.BaseURL + "/auth/google/callback",
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
			Endpoint:     google.Endpoint,
		},
	}

	return s, nil
}

// Middleware wraps the handler with standard middleware (Logging, CORS, Recovery).
func (s *Server) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 1. Recovery
		defer func() {
			if err := recover(); err != nil {
				log.Error("Panic recovered", "error", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()

		// 2. CORS
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

		// 3. Request Logging wrapper
		// We need to wrap ResponseWriter to capture status code
		ww := &responseWriter{w, http.StatusOK}

		h.ServeHTTP(ww, r)

		if !strings.Contains(r.URL.Path, "/.well-known") {

			timesince := time.Since(start)
			str := timesince.String()

			switch {
			case timesince > time.Second*2:
				str = timeUrgentWarn.Render(str)
			case timesince > time.Millisecond*100:
				str = timeWarn.Render(str)

			}

			log.Info(fmt.Sprintf("%s %s %s %d %s", r.Method, r.URL.Path, r.RemoteAddr, ww.statusCode, str))
		}
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
