# NavalPlan

![NavalPlan Screenshot](images/screenshot.png)

NavalPlan is a modern web application for planning sailing voyages, researching maritime stops, and discovering new destinations using a suite of AI agents.

---

## 🛠️ Prerequisites

Before getting started, ensure you have the following installed:

* **Go** (1.22 or later)
* **Node.js** (20 or later) & **npm**
* **Python** (3.10 or later) & `venv` (for ADK agent evaluations)
* **Podman** or **Docker** (for running the local PostgreSQL container)
* **Google Cloud SDK** (`gcloud`) - Optional, for deployment and Cloud SQL management

---

## 🚀 Getting Started

1. **Clone the repository:**
   ```bash
   git clone https://github.com/your-username/navalplan.git
   cd navalplan
   ```

2. **Run Setup:**
   Installs Go dependencies (vendored) and Node.js packages, and creates `.env` from `.env.example`:
   ```bash
   make setup
   ```

3. **Setup Agent Evaluation Environment (ADK):**
   Install the Google Agent Developer Kit (ADK) in a virtual environment:
   ```bash
   python -m venv venv
   source venv/bin/activate
   make setup-adk
   ```

4. **Configure Environment:**
   Edit `.env` and fill in required keys:
   * `GEMINI_API_KEY`: Required for Gemini model access in agent services.
   * `GOOGLE_CLIENT_ID` & `GOOGLE_CLIENT_SECRET`: OAuth credentials from [Google Cloud Console](https://console.cloud.google.com/apis/credentials).
   * `NAVALPLAN_FRONTEND_MAPS_API_KEY` & `NAVALPLAN_BACKEND_MAPS_API_KEY`: API keys from [Google Maps Platform](https://developers.google.com/maps).
   * `NAVALPLAN_MAP_ID`: Google Maps Vector Map ID.
   * `NAVALPLAN_SYSTEM_KEY`: Secure token for internal system/scheduler API endpoints.

5. **Start the Database:**
   Launches a PostgreSQL 15 container via Podman/Docker, runs database migrations, and applies seeds:
   ```bash
   make db-start
   ```

6. **Start Development Stack:**
   Concurrently runs the Go API backend, frontend Vite build/proxy, and Researcher Agent service:
   ```bash
   make dev
   ```
   Open `http://localhost:8080` in your browser.

---

## 🧰 Development & Operations Commands

### Application Lifecycle

| Command | Description |
| :--- | :--- |
| `make dev` | Starts local database, builds frontend assets, and launches Backend & Agent services concurrently. |
| `make run-backend` | Runs only the Go backend server (port 8080). |
| `make run-frontend` | Runs the frontend development server via Vite. |
| `make run-agent` | Runs the Go Researcher Agent microservice (port 8081). |
| `make build-js` | Installs frontend dependencies and builds production JS bundles into `static.min`. |

### Database & Migrations

| Command | Description |
| :--- | :--- |
| `make db-start` | Starts PostgreSQL container, applies migrations (`migrate-up`), and seeds data. |
| `make db-stop` | Stops and removes the local database container. |
| `make db-reset` | Wipes, restarts, and re-seeds the local database. |
| `make db-console` | Opens interactive `psql` shell into the local database container. |
| `make migrate-up` | Applies pending database schema migrations from `code/app/db/migrations`. |
| `make migrate-create` | Interactive prompt to generate a new timestamped SQL migration pair (`.up.sql` / `.down.sql`). |
| `make add-admin EMAIL=user@example.com` | Promotes an existing user or creates an invitation with admin privileges. |

### Utilities & Discovery

| Command | Description |
| :--- | :--- |
| `make dev-mine` | Triggers discovery mining for all months via system API. |
| `make dev-prune` | Triggers seasonal data pruning via system API. |

---

## 🧪 Testing & Agent Evaluations

### Unit Tests
* **Backend + Frontend Unit Tests:**
  ```bash
  make test
  ```
* **Backend Tests Only:** `make test-backend`
* **Frontend Tests Only:** `make test-frontend`

### Agent Trajectory Evaluations (ADK)
NavalPlan uses Google's **Agent Developer Kit (ADK)** evaluation framework to test AI agent trajectories against golden test datasets:

```bash
# Run all agent evaluations
make eval-all

# Run specific agent evaluation
make eval-harbourmaster
make eval-pilot
make eval-commodore
make eval-specialist

# Verbose evaluation output with detailed trace logs
make eval-harbourmaster VERBOSE=1
```

---

## 🏗️ Architecture Overview

![Architecture Diagram](images/architecture.png)

NavalPlan follows a decoupled, performance-first architecture:

* **Backend (`code/app/backend`)**:
  * Written in **Go 1.22+** using the standard library `http.ServeMux` with method matching and modular middleware.
  * Structured into strict layers: `server/` (router/middleware), `handlers/` (HTTP orchestration), `datastore/` (SQL execution), and `models/` (domain structs).
  * Uses `pgx` driver and raw SQL with `golang-migrate` versioning.

* **Frontend (`code/app/frontend`)**:
  * Single Page Application (SPA) built with Vanilla JavaScript (ES Modules) and modern CSS design tokens.
  * Bundled with **Vite**; proxies API calls to Go backend during development.

* **AI Agent Services (`code/services/researcher`)**:
  * Microservice in Go orchestrating Gemini LLM models across specialized agents:
    * **Harbourmaster**: Central coordinator for nautical operations.
    * **Pilot**: Navigation, route planning, and local maritime rules expert.
    * **Commodore**: High-level strategic planning and regional discovery.
    * **Specialist**: Technical maritime task execution and weather/tide analysis.
    * **Lookout**: Continuous safety monitoring and audit agent.
  * Exposes App-to-Agent (A2A) HTTP endpoints, Model Context Protocol (MCP) tool bindings, and Google Agent Registry integration.

---

## 🚀 Deployment

For cloud infrastructure setup and deployment instructions on Google Cloud Platform, see the [Deployment Guide](docs/DEPLOYMENT.md).

Quick cloud deployment targets:
* `make setup-infra`: Provisions GCP services (Cloud Run, Cloud SQL, Storage, APIs).
* `make setup-secrets`: Configures secret keys in Secret Manager.
* `make deploy-backend`: Submits Cloud Build job for main application deployment.
* `make deploy-agent`: Submits Cloud Build job for researcher agent deployment.
* `make deploy-agent-runtime`: Deploys agent service to Vertex AI Agent Runtime.
* `make register-registry`: Registers all agent services with Google Agent Registry.
