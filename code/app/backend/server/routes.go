package server

import (
	"encoding/json"
	"log/slog"
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
		case 2:
			finalHandler = s.requireAuth(s.requireAdmin(s.enforceCSRF(route.Handler)))
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
		{http.MethodGet, "/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		}), 0},
		{http.MethodGet, "/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			status := "ok"
			if err := s.Handler.CheckAgentHealth(r.Context()); err != nil {
				slog.WarnContext(r.Context(), "Agent health check failed", "error", err)
				status = "degraded"
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": status})
		}), 0},
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
		{http.MethodPost, "/api/v1/voyages/{id}/checkin", http.HandlerFunc(s.Handler.Checkin), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/extend", http.HandlerFunc(s.Handler.ExtendVoyage), 1},
		{http.MethodPatch, "/api/v1/voyages/{id}/config", http.HandlerFunc(s.Handler.UpdateVoyageConfig), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/share", http.HandlerFunc(s.Handler.EnableSharing), 1},
		{http.MethodDelete, "/api/v1/voyages/{id}/share", http.HandlerFunc(s.Handler.DisableSharing), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/research_guide", s.rateLimit(5, time.Minute)(http.HandlerFunc(s.Handler.TriggerGuideResearch)), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/research", s.rateLimit(5, time.Minute)(http.HandlerFunc(s.Handler.TriggerFullVoyageResearch)), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/guide", http.HandlerFunc(s.Handler.GetVoyageGuide), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/pilot_report", http.HandlerFunc(s.Handler.GetPilotReport), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/briefings", http.HandlerFunc(s.Handler.ListVoyageBriefings), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/recommendations/generate", s.rateLimit(5, time.Minute)(http.HandlerFunc(s.Handler.GenerateRecommendations)), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/recommendations/stream", http.HandlerFunc(s.Handler.StreamRecommendations), 1},
		{http.MethodGet, "/api/v1/progress/stream", http.HandlerFunc(s.Handler.StreamProgress), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/recommendations", http.HandlerFunc(s.Handler.ListRecommendations), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/guide/snapshot", http.HandlerFunc(s.Handler.UploadVoyageSnapshot), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/map_image", http.HandlerFunc(s.Handler.GetVoyageMapImage), 0}, // Public (Mixed Auth)

		// Stops
		{http.MethodGet, "/api/v1/voyages/{id}/stops", http.HandlerFunc(s.Handler.ListStops), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/stops", http.HandlerFunc(s.Handler.CreateStop), 1},
		{http.MethodPut, "/api/v1/stops/{id}", http.HandlerFunc(s.Handler.UpdateStop), 1},
		{http.MethodDelete, "/api/v1/stops/{id}", http.HandlerFunc(s.Handler.DeleteStop), 1},
		{http.MethodPost, "/api/v1/stops/{id}/research", s.rateLimit(10, time.Minute)(http.HandlerFunc(s.Handler.TriggerResearch)), 1},
		{http.MethodGet, "/api/v1/stops/{id}/briefing", http.HandlerFunc(s.Handler.GetBriefing), 1},
		{http.MethodPost, "/api/v1/stops/{id}/lookout", s.rateLimit(10, time.Minute)(http.HandlerFunc(s.Handler.TriggerLookoutAudit)), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/lookout", s.rateLimit(5, time.Minute)(http.HandlerFunc(s.Handler.TriggerVoyageLookout)), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/weather", s.rateLimit(5, time.Minute)(http.HandlerFunc(s.Handler.UpdateVoyageWeather)), 1},

		// --- Admin ---
		{http.MethodGet, "/api/admin/users", http.HandlerFunc(s.Handler.ListUsers), 2},
		{http.MethodPost, "/api/admin/invite", http.HandlerFunc(s.Handler.InviteUser), 2},
		{http.MethodDelete, "/api/admin/invite/{email}", http.HandlerFunc(s.Handler.RevokeInvitation), 2},
		{http.MethodPost, "/api/admin/weather/update-future", http.HandlerFunc(s.Handler.UpdateAllFutureWeather), 2},
		{http.MethodPost, "/api/admin/lookout/audit", http.HandlerFunc(s.Handler.RunLookoutAuditEndpoint), 2},
		{http.MethodPost, "/api/admin/maintenance", http.HandlerFunc(s.Handler.ScheduledMaintenance), 2},

		// --- Discovery (The Commodore) ---
		{http.MethodGet, "/api/v1/discovery/regions", http.HandlerFunc(s.Handler.GetDiscoveryRegions), 0}, // Public
		{http.MethodPost, "/api/v1/discovery/mine", http.HandlerFunc(s.Handler.DiscoveryMining), 1},       // Protected
		{http.MethodPost, "/api/v1/discovery/prune", http.HandlerFunc(s.Handler.DiscoveryPruning), 1},     // Protected
		{http.MethodDelete, "/api/admin/discovery/regions/{regionID}/months/{month}", http.HandlerFunc(s.Handler.DeleteDiscoveryRegionSeasonality), 1},
		{http.MethodDelete, "/api/v1/discovery/regions/{regionID}/months/{month}", http.HandlerFunc(s.Handler.DeleteDiscoveryRegionSeasonality), 1},

		// --- Static Pages ---
		{http.MethodGet, "/help", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, filepath.Join(staticPath, "help.html"))
		}), 0},

		// --- Static Files Catch-All (Public) ---
		{http.MethodGet, "/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fpath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
			http.ServeFile(w, r, fpath)
		}), 0},

		{http.MethodGet, "/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. API Guard
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}

			// 2. SPA Fallback: If it's a known client-side route or has no extension
			if strings.HasPrefix(r.URL.Path, "/voyages/") ||
				strings.HasPrefix(r.URL.Path, "/shared/") ||
				filepath.Ext(r.URL.Path) == "" {
				http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
				return
			}

			// 3. Static Files
			fpath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
			http.ServeFile(w, r, fpath)
		}), 0},
	}

	s.Register(routes...)
}
