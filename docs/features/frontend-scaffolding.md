# Agent Task: NavalPlan Frontend Scaffolding

**Goal:** Initialize the frontend application for **NavalPlan**, a sailing voyage planner. This application must mirror the architecture and stack of its peer application, *Navallog*.

**Stack:**
* **Build Tool:** Vite
* **Language:** Vanilla JavaScript (ES Modules)
* **Maps:** Mapbox GL JS
* **Dates:** Luxon
* **Testing:** Jasmine (Browser Runner)
* **Styling:** Native CSS (CSS Variables)

**Directory Root:** `code/app/frontend`

---

### Step 1: Project Structure & Configuration

Create the following directory structure inside `code/app/frontend`:
```text
code/app/frontend/
├── css/
│   └── main.css
├── js/
│   ├── lib/
│   └── main.js
├── public/
├── index.html
├── package.json
└── vite.config.js

```

#### 1.1 Dependencies (`package.json`)

Create `package.json` with the following configuration. Ensure `type` is NOT set to module to avoid CommonJS conflicts with some tooling, or handle accordingly.

```json
{
  "name": "navalplan-frontend",
  "version": "0.0.1",
  "description": "Frontend for NavalPlan",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview",
    "test": "jasmine-browser-runner runSpecs"
  },
  "dependencies": {
    "luxon": "^3.4.4",
    "mapbox-gl": "^3.1.2"
  },
  "devDependencies": {
    "jasmine-browser-runner": "^2.3.0",
    "jasmine-core": "^5.1.1",
    "vite": "^5.0.12"
  }
}

```

#### 1.2 Build Configuration (`vite.config.js`)

Configure Vite to proxy API requests to the Go backend running on port 8080.

```javascript
import { defineConfig } from 'vite';

export default defineConfig({
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      '/auth': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});

```

---

### Step 2: Core Application Files

#### 2.1 Entry Point (`index.html`)

Create a semantic HTML5 structure with a sidebar-layout.

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>NavalPlan: Voyage Planner</title>
    
    <link href="[https://api.mapbox.com/mapbox-gl-js/v3.1.2/mapbox-gl.css](https://api.mapbox.com/mapbox-gl-js/v3.1.2/mapbox-gl.css)" rel="stylesheet" />
    
    <link rel="stylesheet" href="/css/main.css" />
  </head>
  <body>
    <div id="app">
      <nav id="sidebar">
        <div class="brand">
          <h1>NavalPlan</h1>
        </div>
        
        <div class="sidebar-actions">
           <button id="btn-new-voyage" class="btn primary">+ New Voyage</button>
        </div>

        <div id="voyage-list">
          <p class="loading-text">Loading voyages...</p>
        </div>
      </nav>

      <main id="map-container"></main>
    </div>

    <script type="module" src="/js/main.js"></script>
  </body>
</html>

```

#### 2.2 Base Styles (`css/main.css`)

Implement a clean, responsive split-pane layout using CSS variables.

```css
:root {
  --primary-color: #005f73;
  --secondary-color: #0a9396;
  --text-color: #333;
  --bg-color: #f4f4f4;
  --sidebar-width: 320px;
  --header-height: 60px;
}

body {
  margin: 0;
  padding: 0;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  height: 100vh;
  width: 100vw;
  overflow: hidden;
  color: var(--text-color);
}

#app {
  display: flex;
  height: 100%;
  width: 100%;
}

/* Sidebar Styles */
#sidebar {
  width: var(--sidebar-width);
  background: white;
  border-right: 1px solid #ddd;
  display: flex;
  flex-direction: column;
  box-shadow: 2px 0 5px rgba(0,0,0,0.05);
  z-index: 10;
}

.brand {
  padding: 1rem;
  border-bottom: 1px solid #eee;
}

.brand h1 {
  margin: 0;
  color: var(--primary-color);
  font-size: 1.5rem;
}

.sidebar-actions {
  padding: 1rem;
}

#voyage-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 1rem;
}

/* Map Styles */
#map-container {
  flex: 1;
  background-color: #e5e5e5; /* Fallback */
  position: relative;
}

/* Components */
.btn {
  width: 100%;
  padding: 0.75rem 1rem;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-weight: 600;
  transition: background 0.2s;
}

.btn.primary {
  background-color: var(--primary-color);
  color: white;
}

.btn.primary:hover {
  background-color: var(--secondary-color);
}

.loading-text {
  color: #888;
  font-style: italic;
  text-align: center;
  margin-top: 2rem;
}

```

#### 2.3 Application Logic (`js/main.js`)

Initialize the application and Mapbox map.

*Note: Ensure you include a placeholder for the Mapbox Access Token.*

```javascript
import mapboxgl from 'mapbox-gl';
// import { DateTime } from 'luxon'; // Ready for use later

// Configuration
// TODO: Replace with env variable logic in production
const MAPBOX_TOKEN = 'pk.eyJ1IjoidHByeWFuIiwiYSI6ImNqMDd1bXk2ZzA0MGMzM3FvM3FvM3FvIn0.ABC-123'; 

document.addEventListener('DOMContentLoaded', () => {
  initApp();
});

function initApp() {
  console.log('NavalPlan: Initializing...');
  initMap();
}

function initMap() {
  if (!MAPBOX_TOKEN) {
    console.error('Mapbox token is missing.');
    return;
  }

  mapboxgl.accessToken = MAPBOX_TOKEN;

  const map = new mapboxgl.Map({
    container: 'map-container',
    style: 'mapbox://styles/mapbox/outdoors-v12', 
    center: [-123.0, 48.5], // Salish Sea
    zoom: 8
  });

  map.on('load', () => {
    console.log('NavalPlan: Map Loaded Successfully');
    // Future: Load voyages from API here
  });

  // Add navigation controls (zoom/rotate)
  map.addControl(new mapboxgl.NavigationControl());
}

```

---

### Step 3: Execution

1. Initialize the project: `npm install`
2. Run the development server: `npm run dev`
3. Verify that `http://localhost:5173` loads and displays the layout.
