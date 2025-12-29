package server

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"app/datastore"
	"app/server/handlers"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Server struct {
	Router *chi.Mux
	DB     datastore.Store
}

func New(db datastore.Store) (*Server, error) {
	log.SetOutput(os.Stderr)
	log.SetPrefix("backend")
	r := chi.NewRouter()
	docsService := handlers.NewGoogleDocsService()
	h := handlers.New(db, docsService)

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

	// API Routes (Placeholder)
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/voyages", h.ListVoyages)
		r.Post("/voyages", h.CreateVoyage)
		r.Get("/voyages/{id}", h.GetVoyage)
		r.Put("/voyages/{id}", h.UpdateVoyage)
		r.Delete("/voyages/{id}", h.DeleteVoyage)
		r.Post("/voyages/{id}/export", h.ExportVoyage)
		r.Post("/voyages/{id}/share", h.EnableSharing)
		r.Delete("/voyages/{id}/share", h.DisableSharing)

		r.Get("/public/voyages/{token}", h.GetPublicVoyage)
		r.Get("/public/voyages/{token}/stops", h.GetPublicStops)

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

	return &Server{
		Router: r,
		DB:     db,
	}, nil
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
