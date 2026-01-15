package server

import (
	"net/http"

	"app/config"
	"app/datastore"
	"app/server/handlers"

	"github.com/charmbracelet/log"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Server holds the application dependencies and router.
type Server struct {
	Mux          *http.ServeMux
	DB           datastore.Store
	GoogleConfig *oauth2.Config
	Env          string
	Project      string
	BaseURL      string
	Handler      *handlers.Handler
	SystemAPIKey string
}

// New initializes a new Server with the provided database and configuration.
func New(db datastore.Store, cfg *config.Config) (*Server, error) {

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
		Project:      cfg.Project,
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
	// Apply in reverse order (wrapping)
	h = s.traceMiddleware(h)
	h = s.requestLoggingMiddleware(h)
	h = s.corsMiddleware(h)
	h = s.recoveryMiddleware(h)
	return h
}
