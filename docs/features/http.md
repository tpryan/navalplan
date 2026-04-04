Based on the `navallog` implementation, here is a detailed explanation of the changes required to refactor `navalplan`'s routing to use the same `net/http` table-driven approach.

This refactor involves moving away from the `chi` router to the standard library's `http.ServeMux` (Go 1.22+), defining routes in a structured slice, and centralizing handler registration.

### 1. Create `code/app/backend/server/routes.go`

You need to create a new file `routes.go` in `navalplan`. This file will define the `route` struct, the `Register` method, and the `Routes` configuration method.

**Changes required:**

1. **Define the `route` struct:** This holds the Verb (method), Path, Handler, and AuthLevel.
2. **Implement `Register`:** Iterate through the routes, wrap them in middleware (Auth, Gzip) based on `AuthLevel`, and register them to the `Mux`.
3. **Implement `Routes`:** Define the static file handlers (manually, as `navallog` does) and the slice of API routes mapping your existing `handlers` methods to HTTP paths.

**Code for `code/app/backend/server/routes.go`:**

```go
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

		// Apply other standard middleware (e.g. Gzip)
		// Note: navallog defines GzipHandler in server.go, you may need to copy that too.
		// finalHandler = GzipHandler(finalHandler) 

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
		// Note: Navallog puts the root handler here to handle SPA routing manually
		{http.MethodGet, "/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Manual static file serving logic found in navallog's routes.go
			fpath := filepath.Join(staticPath, filepath.Clean(r.URL.Path))
			info, err := os.Stat(fpath)
			if err != nil {
				if os.IsNotExist(err) {
					w.WriteHeader(http.StatusNotFound)
					http.ServeFile(w, r, filepath.Join(staticPath, "index.html")) // SPA Fallback
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
		{http.MethodGet, "/voyages", indexHandler, 0},
		{http.MethodGet, "/voyages/{id}", indexHandler, 0},
	}

	s.Register(routes...)
}

```

### 2. Update `code/app/backend/server/server.go`

You need to modify the `Server` struct to match `navallog`'s dependencies (specifically holding the handlers and using `http.ServeMux` instead of `chi`) and update the `New` constructor.

**Changes required:**

1. **Struct:** Replace `Router *chi.Mux` with `Mux *http.ServeMux`. Add `Handler *handlers.Handler` so `routes.go` can access the methods.
2. **Imports:** Remove `github.com/go-chi/chi/v5` and `github.com/go-chi/cors`. Add `net/http`.
3. **New function:** Initialize `http.NewServeMux()`. Remove the inline route definitions. Call `s.Routes(cfg.ContentDir)` *inside* or allow the caller to call it (Navallog's `New` does not call `Routes`, it is called in `main`).
4. **Middleware:** Port the global middleware logic (logging) to a `Middleware` method on the Server struct, rather than `r.Use`.

**Code for `code/app/backend/server/server.go`:**

```go
package server

import (
	"app/config"
	"app/datastore"
	"app/server/handlers"
	"net/http"
	"golang.org/x/oauth2"
	"github.com/charmbracelet/log"
	// ... other imports ...
)

type Server struct {
	Mux          *http.ServeMux // Changed from Router *chi.Mux
	DB           datastore.Store
	GoogleConfig *oauth2.Config
	Env          string
	Handler      *handlers.Handler // Added to store the handler instance
}

func New(db datastore.Store, cfg *config.Config) (*Server, error) {
	// ... logging setup ...

	docsService := handlers.NewGoogleDocsService()
	h := handlers.New(db, docsService, cfg.ContentDir, cfg.NavalPlanAgentURL)

	s := &Server{
		Mux:     http.NewServeMux(),
		DB:      db,
		Env:     cfg.Env,
		Handler: h, // Store handler
		GoogleConfig: &oauth2.Config{
			// ... config ...
		},
	}

	// Note: You might need to move CORS and Recovery logic into a custom Middleware 
	// wrapper similar to navallog's `s.Middleware` because http.ServeMux doesn't support `Use()`.
	
	return s, nil
}

// Port the Middleware function from navallog
func (s *Server) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Add CORS headers manually here or wrap with a cors library handler
		// 2. Add Logging logic (from your existing CustomLogger or navallog's implementation)
		
		// navallog example:
		// rec := &responseWriterRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		// h.ServeHTTP(rec, r)
		// log.Info(...)
		
		// Simplified for brevity:
		h.ServeHTTP(w, r)
	})
}

```

### 3. Adjust `main.go` (Implied)

Since `navallog` separates `New` (creation) from `Routes` (registration), ensure your `main.go` calls `server.Routes(cfg.ContentDir)` after creating the server, and wraps the mux in middleware before listening.

```go
// In main.go
srv, _ := server.New(db, cfg)
srv.Routes(cfg.ContentDir)

// Wrap with middleware when starting
http.ListenAndServe(":"+port, srv.Middleware(srv.Mux))

```

### 4. Middleware Compatibility

You will need to ensure `requireAuth` matches the signature `func (s *Server) requireAuth(next http.Handler) http.Handler`. The current implementation in `navalplan` is already very close to this, but check that it writes errors directly rather than relying on `chi` flow control.