package server

import (
	"net/http"
	"os"
	"path/filepath"
)

// route defines a single HTTP route with its verb, path, handler, and auth level.
type route struct {
	Verb      string
	Path      string
	Handler   http.Handler
	AuthLevel int // 0: Public, 1: Protected (Session)
}

// Register registers multiple routes on the server's multiplexer.
func (s *Server) Register(r ...route) {
	for _, route := range r {
		var finalHandler http.Handler = route.Handler

		// Apply Auth Middleware based on level
		switch route.AuthLevel {
		case 1:
			finalHandler = s.requireAuth(route.Handler)
		}

		s.Mux.Handle(route.Verb+" "+route.Path, finalHandler)
	}
}

func (s *Server) Routes(staticPath string) {
	// 1. Define Static File Handlers (matching navallog's manual approach)
	indexHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
	})

	// 2. Define the Route Table
	routes := []route{
		// --- System / Auth (Public) ---
		{http.MethodGet, "/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }), 0},
		{http.MethodGet, "/auth/google/login", http.HandlerFunc(s.oauthGoogleLogin), 0},
		{http.MethodGet, "/auth/google/callback", http.HandlerFunc(s.oauthGoogleCallback), 0},
		{http.MethodGet, "/auth/logout", http.HandlerFunc(s.oauthLogout), 0},

		// --- API Public ---
		{http.MethodGet, "/api/v1/public/voyages/{token}", http.HandlerFunc(s.Handler.GetPublicVoyage), 0},
		{http.MethodGet, "/api/v1/public/voyages/{token}/stops", http.HandlerFunc(s.Handler.GetPublicStops), 0},

		// --- API Protected (Level 1) ---
		// Person
		{http.MethodGet, "/api/v1/person", http.HandlerFunc(s.Handler.GetPerson), 1},
		{http.MethodPut, "/api/v1/person", http.HandlerFunc(s.Handler.UpdatePerson), 1},

		// Voyages
		{http.MethodGet, "/api/v1/voyages", http.HandlerFunc(s.Handler.ListVoyages), 1},
		{http.MethodPost, "/api/v1/voyages", http.HandlerFunc(s.Handler.CreateVoyage), 1},
		{http.MethodGet, "/api/v1/voyages/{id}", http.HandlerFunc(s.Handler.GetVoyage), 1},
		{http.MethodPut, "/api/v1/voyages/{id}", http.HandlerFunc(s.Handler.UpdateVoyage), 1},
		{http.MethodDelete, "/api/v1/voyages/{id}", http.HandlerFunc(s.Handler.DeleteVoyage), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/export", http.HandlerFunc(s.Handler.ExportVoyage), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/share", http.HandlerFunc(s.Handler.EnableSharing), 1},
		{http.MethodDelete, "/api/v1/voyages/{id}/share", http.HandlerFunc(s.Handler.DisableSharing), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/research_guide", http.HandlerFunc(s.Handler.TriggerGuideResearch), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/research", http.HandlerFunc(s.Handler.TriggerFullVoyageResearch), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/guide", http.HandlerFunc(s.Handler.GetVoyageGuide), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/briefings", http.HandlerFunc(s.Handler.ListVoyageBriefings), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/guide/map_image", http.HandlerFunc(s.Handler.UploadVoyageMap), 1},

		// Stops
		{http.MethodGet, "/api/v1/voyages/{id}/stops", http.HandlerFunc(s.Handler.ListStops), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/stops", http.HandlerFunc(s.Handler.CreateStop), 1},
		{http.MethodPut, "/api/v1/stops/{id}", http.HandlerFunc(s.Handler.UpdateStop), 1},
		{http.MethodDelete, "/api/v1/stops/{id}", http.HandlerFunc(s.Handler.DeleteStop), 1},
		{http.MethodPost, "/api/v1/stops/{id}/research", http.HandlerFunc(s.Handler.TriggerResearch), 1},
		{http.MethodGet, "/api/v1/stops/{id}/briefing", http.HandlerFunc(s.Handler.GetBriefing), 1},

		// --- Static Files Catch-All (Public) ---
		{http.MethodGet, "/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fpath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
			info, err := os.Stat(fpath)
			if err != nil {
				if os.IsNotExist(err) {
					// SPA Fallback: If file doesn't exist, serve index.html (client-side routing)
					// But we should verify if it looks like an API call to avoid serving HTML for 404 API
					// For now, simple fallback.
					http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
					return
				}
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			if info.IsDir() {
				index := filepath.Join(fpath, "index.html")
				if _, err := os.Stat(index); err == nil {
					http.ServeFile(w, r, index)
					return
				}
				http.ServeFile(w, r, filepath.Join(staticPath, "index.html")) // SPA Fallback
				return
			}
			http.ServeFile(w, r, fpath)
		}), 0},
		
		// Add explicit SPA routes if necessary to point to indexHandler
		// This ensures deep links work even if static handler misses them
		{http.MethodGet, "/voyages", indexHandler, 0},
		{http.MethodGet, "/voyages/{id}", indexHandler, 0},
	}

	s.Register(routes...)
}
