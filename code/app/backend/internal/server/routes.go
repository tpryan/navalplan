package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type route struct {
	Verb      string
	Path      string
	Handler   http.Handler
	AuthLevel int // 0: Public, 1: Protected (Session), 2: Admin
}

func (s *Server) Register(r ...route) {
	for _, route := range r {
		var finalHandler http.Handler = route.Handler

		switch route.AuthLevel {
		case 1:
			finalHandler = s.requireAuth(s.enforceCSRF(route.Handler))
		case 2:
			finalHandler = s.requireAuth(s.requireAdmin(s.enforceCSRF(route.Handler)))
		}

		finalHandler = s.staticCache(s.secureHeaders(finalHandler))
		s.Mux.Handle(route.Verb+" "+route.Path, s.gzipMiddleware(finalHandler))
	}
}

func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if err := s.Handler.CheckAgentHealth(r.Context()); err != nil {
		slog.WarnContext(r.Context(), "Agent health check failed", "error", err)
		status = "degraded"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func (s *Server) Routes(staticPath string) {
	routes := []route{
		// --- System / Auth (Public) ---
		{http.MethodGet, "/healthz", http.HandlerFunc(s.healthCheck), 0},
		{http.MethodGet, "/health", http.HandlerFunc(s.healthCheck), 0},
		{http.MethodGet, "/auth/google/login", http.HandlerFunc(s.oauthGoogleLogin), 0},
		{http.MethodGet, "/auth/google/callback", http.HandlerFunc(s.oauthGoogleCallback), 0},
		{http.MethodPost, "/auth/logout", http.HandlerFunc(s.oauthLogout), 1},

		// --- API Public ---
		{http.MethodGet, "/api/v1/public/voyages/{token}", http.HandlerFunc(s.Handler.GetPublicVoyage), 0},
		{http.MethodGet, "/api/v1/public/voyages/{token}/stops", http.HandlerFunc(s.Handler.GetPublicStops), 0},
		{http.MethodGet, "/api/v1/public/voyages/{token}/guide", http.HandlerFunc(s.Handler.GetPublicVoyageGuide), 0},
		{http.MethodGet, "/api/v1/public/voyages/{token}/track", http.HandlerFunc(s.Handler.GetPublicVoyageTracks), 0},
		{http.MethodGet, "/api/v1/public/voyages/{token}/tracks", http.HandlerFunc(s.Handler.GetPublicVoyageTracks), 0},

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
		{http.MethodGet, "/api/v1/voyages/{id}/map_image", http.HandlerFunc(s.Handler.GetVoyageMapImage), 0},

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

		// Tracks
		{http.MethodGet, "/api/v1/voyages/{id}/track", http.HandlerFunc(s.Handler.ListVoyageTracks), 1},
		{http.MethodGet, "/api/v1/voyages/{id}/tracks", http.HandlerFunc(s.Handler.ListVoyageTracks), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/track", http.HandlerFunc(s.Handler.UploadVoyageTrack), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/tracks", http.HandlerFunc(s.Handler.UploadVoyageTrack), 1},
		{http.MethodDelete, "/api/v1/voyages/{id}/track/{trackId}", http.HandlerFunc(s.Handler.DeleteVoyageTrack), 1},
		{http.MethodDelete, "/api/v1/voyages/{id}/tracks/{trackId}", http.HandlerFunc(s.Handler.DeleteVoyageTrack), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/track/{trackId}/debrief", http.HandlerFunc(s.Handler.DebriefVoyageTrack), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/tracks/{trackId}/debrief", http.HandlerFunc(s.Handler.DebriefVoyageTrack), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/stops/{stopId}/track", http.HandlerFunc(s.Handler.UploadStopTrack), 1},
		{http.MethodPost, "/api/v1/voyages/{id}/stops/{stopId}/tracks", http.HandlerFunc(s.Handler.UploadStopTrack), 1},

		// --- Admin ---

		{http.MethodGet, "/api/admin/users", http.HandlerFunc(s.Handler.ListUsers), 2},
		{http.MethodPost, "/api/admin/invite", http.HandlerFunc(s.Handler.InviteUser), 2},
		{http.MethodDelete, "/api/admin/invite/{email}", http.HandlerFunc(s.Handler.RevokeInvitation), 2},
		{http.MethodPost, "/api/admin/weather/update-future", http.HandlerFunc(s.Handler.UpdateAllFutureWeather), 2},
		{http.MethodPost, "/api/admin/lookout/audit", http.HandlerFunc(s.Handler.RunLookoutAuditEndpoint), 2},
		{http.MethodPost, "/api/admin/maintenance", http.HandlerFunc(s.Handler.ScheduledMaintenance), 2},

		// --- Discovery (The Commodore) ---
		{http.MethodGet, "/api/v1/discovery/regions", http.HandlerFunc(s.Handler.GetDiscoveryRegions), 0},
		{http.MethodPost, "/api/v1/discovery/mine", http.HandlerFunc(s.Handler.DiscoveryMining), 1},
		{http.MethodPost, "/api/v1/discovery/prune", http.HandlerFunc(s.Handler.DiscoveryPruning), 1},
		{http.MethodDelete, "/api/admin/discovery/regions/{regionID}/months/{month}", http.HandlerFunc(s.Handler.DeleteDiscoveryRegionSeasonality), 1},
		{http.MethodDelete, "/api/v1/discovery/regions/{regionID}/months/{month}", http.HandlerFunc(s.Handler.DeleteDiscoveryRegionSeasonality), 1},

		// --- Static Pages ---
		{http.MethodGet, "/help", s.helpHandler(staticPath), 0},

		// --- Static Files Catch-All (Public) ---
		{http.MethodGet, "/assets/", s.assetsHandler(staticPath), 0},
		{http.MethodGet, "/", s.spaHandler(staticPath), 0},
	}

	s.Register(routes...)
}

func (s *Server) helpHandler(staticPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticPath, "help.html"))
	}
}

func (s *Server) assetsHandler(staticPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fpath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
		http.ServeFile(w, r, fpath)
	}
}

func (s *Server) spaHandler(staticPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/voyages/") ||
			strings.HasPrefix(r.URL.Path, "/shared/") ||
			strings.HasPrefix(r.URL.Path, "/discover") ||
			filepath.Ext(r.URL.Path) == "" {
			http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
			return
		}

		fpath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
		http.ServeFile(w, r, fpath)
	}
}
