package server

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"app/config"
	"app/datastore"
	"app/server/handlers"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Server struct {
	Router       *chi.Mux
	DB           datastore.Store
	GoogleConfig *oauth2.Config
	Env          string
}

func New(db datastore.Store, cfg *config.Config) (*Server, error) {
	log.SetOutput(os.Stderr)
	log.SetPrefix("backend")

	s := &Server{
		DB:  db,
		Env: cfg.Env,
		GoogleConfig: &oauth2.Config{
			RedirectURL:  cfg.GoogleRedirectURL,
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
			Endpoint:     google.Endpoint,
		},
	}

	r := chi.NewRouter()
	docsService := handlers.NewGoogleDocsService()
	h := handlers.New(db, docsService, cfg.ContentDir, cfg.NavalPlanAgentURL)

	// Standard Middleware
	r.Use(CustomLogger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Basic Health Check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Auth Routes
	r.Get("/auth/google/login", s.oauthGoogleLogin)
	r.Get("/auth/google/callback", s.oauthGoogleCallback)
	r.Get("/auth/logout", s.oauthLogout)

	// API Routes (Placeholder)
	r.Route("/api/v1", func(r chi.Router) {
		// --- Public Routes ---
		r.Get("/public/voyages/{token}", h.GetPublicVoyage)
		r.Get("/public/voyages/{token}/stops", h.GetPublicStops)

		// --- Protected Routes ---
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			// Person
			r.Get("/person", h.GetPerson)
			r.Put("/person", h.UpdatePerson)

			// Voyages
			r.Get("/voyages", h.ListVoyages)
			r.Post("/voyages", h.CreateVoyage)
			r.Get("/voyages/{id}", h.GetVoyage)
			r.Put("/voyages/{id}", h.UpdateVoyage)
			r.Delete("/voyages/{id}", h.DeleteVoyage)
			r.Post("/voyages/{id}/export", h.ExportVoyage)
			r.Post("/voyages/{id}/share", h.EnableSharing)
			r.Delete("/voyages/{id}/share", h.DisableSharing)

			r.Post("/voyages/{id}/research_guide", h.TriggerGuideResearch)
			r.Post("/voyages/{id}/research", h.TriggerFullVoyageResearch)
			r.Get("/voyages/{id}/guide", h.GetVoyageGuide)
			r.Post("/voyages/{id}/guide/map_image", h.UploadVoyageMap)

			// Stop Management
			r.Route("/voyages/{id}/stops", func(r chi.Router) {
				r.Get("/", h.ListStops)
				r.Post("/", h.CreateStop)
			})
			r.Route("/stops/{id}", func(r chi.Router) {
				r.Put("/", h.UpdateStop)
				r.Delete("/", h.DeleteStop)
				r.Post("/research", h.TriggerResearch)
				r.Get("/briefing", h.GetBriefing)
			})
		})
	})

	s.Router = r
	return s, nil
}

func CustomLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		// Calculate duration
		str := time.Since(start).String()

		// Log
		log.Info(fmt.Sprintf("%s %s %s %d %s", r.Method, r.URL.Path, r.RemoteAddr, ww.Status(), str))
	})
}

func (s *Server) Routes(contentDir string) {
	// 404 Handler for API
	s.Router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, contentDir+"/index.html")
	})

	// Static Files
	fileServer := http.FileServer(http.Dir(contentDir))
	s.Router.Handle("/*", http.StripPrefix("/", fileServer))
}
