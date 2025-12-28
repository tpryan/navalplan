package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	Router *chi.Mux
	// DB *datastore.DB // We will uncomment this when we build the datastore package
}

func New() (*Server, error) {
	r := chi.NewRouter()

	// Standard Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Cors) // You might need to configure this specifically

	// Basic Health Check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// API Routes (Placeholder)
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/voyages", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"message": "Voyages endpoint coming soon"}`))
		})
	})

	return &Server{
		Router: r,
	}, nil
}
