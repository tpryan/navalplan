### 1. The Makefile

We need to update the `Makefile` to include the `build-js` step and ensure `run` depends on it.

```makefile
# Makefile
PROJECT_NAME=navalplan

# ... (Database variables from before) ...

.PHONY: run build build-js clean-static

# 1. RUN: Builds the frontend first, then runs Go serving that static folder
run: build
	@echo "Starting NavalPlan (Production Mode)..."
	cd code/app/backend && go run main.go --content=./static.min

# 2. BUILD: The master build command
build: build-js

# 3. CLEAN: Removes the old static files from the backend
clean-static:
	rm -rf code/app/backend/static.min

# 4. BUILD-JS: Installs deps and runs Vite Build
build-js: clean-static
	@echo "Building Frontend..."
	cd code/app/frontend && npm install
	cd code/app/frontend && npm run build

```

### 2. Frontend Config (`code/app/frontend/vite.config.js`)

This configuration ensures that when `npm run build` is called by the Makefile, the files land exactly where the Go server expects them (`../backend/static.min`).

```javascript
import { defineConfig } from 'vite';

export default defineConfig({
  // Dev Config (for when you run 'npm run dev' manually)
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    }
  },
  // Build Config (Triggered by 'make run')
  build: {
    // This pushes the compiled HTML/JS/CSS directly into the Backend folder
    outDir: '../backend/static.min',
    emptyOutDir: true,
    sourcemap: true,
  },
});

```

### 3. Backend Entry Point (`code/app/backend/main.go`)

We need to update `main.go` to parse the `--content` flag (just like `Navallog` does) so it knows which folder to serve.

```go
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/tpryan/navalplan/code/app/backend/server"
)

func main() {
	// 1. Parse Flags (Matching Navallog's pattern)
	// Usage: go run main.go --content=./static.min
	contentDir := flag.String("content", "./static.min", "Path to static content to serve")
	flag.Parse()

	port := os.Getenv("NAVALPLAN_PORT")
	if port == "" {
		port = "8080"
	}

	// 2. Initialize Server
	srv, err := server.New()
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	// 3. Register Routes (Passing the content dir)
	// This tells the router: "If it's not an API call, look for files in *contentDir*"
	srv.Routes(*contentDir)

	log.Printf("NavalPlan starting on port %s...", port)
	log.Printf("Serving static content from: %s", *contentDir)
	
	// 4. Start
	http.ListenAndServe(":"+port, srv.Router)
}

```

### Summary of Workflow

With these changes, your workflow mirrors `Navallog`:

1. **To Run the Full App:**
* Command: `make run`
* Action: It compiles your JS/CSS -> puts it in `backend/static.min` -> Starts Go.
* Result: You open `localhost:8080` and see the *production-ready* app.


2. **To Develop Frontend (Hot Reload):**
* Command: `cd code/app/frontend && npm run dev`
* Action: Starts Vite dev server.
* Result: You open `localhost:5173`. It proxies API calls to Go (running in another terminal), but gives you instant updates for JS/CSS changes.