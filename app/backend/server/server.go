package server

import (
	"net/http"

	"app/datastore"
	"app/server/handlers"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Server struct {
	Router *chi.Mux
	DB     *datastore.DB
}

func New(db *datastore.DB) (*Server, error) {
	r := chi.NewRouter()
	h := handlers.New(db)

	// Standard Middleware
	r.Use(middleware.Logger)
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
		r.Delete("/voyages/{id}", h.DeleteVoyage)
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
