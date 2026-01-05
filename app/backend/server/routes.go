package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"
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
			finalHandler = s.requireAuth(s.enforceCSRF(route.Handler))
		}

		// Apply static cache and security headers
		finalHandler = s.staticCache(s.secureHeaders(finalHandler))

		// Apply gzip at the outermost level
		s.Mux.Handle(route.Verb+" "+route.Path, s.gzipMiddleware(finalHandler))
	}
}

func (s *Server) Routes(staticPath string) {
	// 1. Define the Route Table
	routes := []route{
		// --- System / Auth (Public) ---
		{http.MethodGet, "/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }), 0},
		{http.MethodGet, "/auth/google/login", http.HandlerFunc(s.oauthGoogleLogin), 0},
		{http.MethodGet, "/auth/google/callback", http.HandlerFunc(s.oauthGoogleCallback), 0},
		{http.MethodPost, "/auth/logout", http.HandlerFunc(s.oauthLogout), 1},

		// --- API Public ---
		{http.MethodGet, "/api/v1/public/voyages/{token}", http.HandlerFunc(s.Handler.GetPublicVoyage), 0},
		{http.MethodGet, "/api/v1/public/voyages/{token}/stops", http.HandlerFunc(s.Handler.GetPublicStops), 0},
		{http.MethodGet, "/api/v1/public/voyages/{token}/guide", http.HandlerFunc(s.Handler.GetPublicVoyageGuide), 0},

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
		{http.MethodPost, "/api/v1/voyages/{id}/research_guide", s.rateLimit(5, time.Minute)(http.HandlerFunc(s.Handler.TriggerGuideResearch)), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/research", s.rateLimit(5, time.Minute)(http.HandlerFunc(s.Handler.TriggerFullVoyageResearch)), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/guide", http.HandlerFunc(s.Handler.GetVoyageGuide), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/briefings", http.HandlerFunc(s.Handler.ListVoyageBriefings), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/guide/snapshot", http.HandlerFunc(s.Handler.UploadVoyageSnapshot), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/map_image", http.HandlerFunc(s.Handler.GetVoyageMapImage), 0}, // Public (Mixed Auth)

		// Stops
		{http.MethodGet, "/api/v1/voyages/{id}/stops", http.HandlerFunc(s.Handler.ListStops), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/stops", http.HandlerFunc(s.Handler.CreateStop), 1},
		{http.MethodPut, "/api/v1/stops/{id}", http.HandlerFunc(s.Handler.UpdateStop), 1},
		{http.MethodDelete, "/api/v1/stops/{id}", http.HandlerFunc(s.Handler.DeleteStop), 1},
		{http.MethodPost, "/api/v1/stops/{id}/research", s.rateLimit(10, time.Minute)(http.HandlerFunc(s.Handler.TriggerResearch)), 1},
		{http.MethodGet, "/api/v1/stops/{id}/briefing", http.HandlerFunc(s.Handler.GetBriefing), 1},

		// --- Discovery (The Commodore) ---
		{http.MethodGet, "/api/v1/discovery/regions", http.HandlerFunc(s.Handler.GetDiscoveryRegions), 0}, // Public
		{http.MethodPost, "/api/v1/discovery/mine", http.HandlerFunc(s.Handler.DiscoveryMining), 1},      // Protected

		// --- Static Files Catch-All (Public) ---
		{http.MethodGet, "/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. API Guard: Don't serve HTML for missing API routes
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}

			// 2. SPA Fallback: If no extension, assume it's a client-side route
			// This avoids os.Stat overhead for routes like /dashboard, /users/123
			if filepath.Ext(r.URL.Path) == "" {
				http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
				return
			}

			// 3. Static Files: Serve directly
			// http.ServeFile handles 404s if the file (with extension) doesn't exist
			fpath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
			http.ServeFile(w, r, fpath)
		}), 0},
	}

	s.Register(routes...)
}
