# Agent Handbook for NavalPlan

This document synthesizes project conventions, architectural understandings, and non-obvious operational details to accelerate agent effectiveness within the repository.

## 🚀 Core Workflow & Development Commands

The project manages a complex tripartite stack: a React/Vite Frontend, a Go API Backend, and dedicated Agent Services. State management around these components is critical.

### Primary Lifecycle Commands (Via `make`)

*   **Development/Local Run**:
    *   `make dev`: **The primary development command.** It orchestrates starting the Database container (using Podman), building the static frontend assets, and then concurrently running the Go backend, the Agent service, and the frontend development server. It uses `&` and `wait` to manage multiple long-running processes.
    *   `make setup`: Runs `npm install` and `go mod tidy` for both frontend and backend, ensuring all external dependencies are met before running development commands.
*   **Testing**:
    *   `make test`: Runs the full test suite, covering both unit tests (`go test`) for the backend/services and end-to-end tests (`npm test`) for the frontend.
*   **Deployment**:
    *   **Frontend**: Build is handled by `make build-js` (triggers `npm run build` in `code/app/frontend`).
    *   **Backend**: Deployment uses `gcloud builds submit --config cloudbuild.yaml .`. Secrets are managed via `Makefile` variables (`PROD_DB_USER`, etc.).
    *   **Database/Migrations**: Database lifecycle is *highly* separated:
        *   **Local Dev**: Use `make db-start` (relies on Podman) followed by `make migrate-up`.
        *   **Production**: Never use the development commands. Must use `make migrate-prod` or `make deploy-sql`. **The production path requires specific `gcloud` credentials and Cloud SQL Auth Proxy setup.**

### ⚠️ Gotchas & Implicit Conventions

1.  **Environment Variables**: The project is deeply reliant on a `.env` file. Key secrets (`NAVALPLAN_SYSTEM_KEY`, `NAVALPLAN_FRONTEND_MAPS_API_KEY`, etc.) must be set for *any* operation to succeed outside of pure unit tests.
2.  **Agent Evaluation Complexity**: The agent evaluation flow (`eval-agent` target in Makefile) is a manual, multi-step shell script sequence. It:
    *   Forces the copying of the `.env` file into `code/services/researcher/.env`.
    *   Injects code into `code/services/researcher/__init__.py` and `code/services/researcher/agent.py` at runtime.
    *   Executes the agent using the `google-adk` CLI with specific paths (`code/services/researcher/eval/$$AGENT/$$AGENT.test.json`).
    *   It assumes the agent service runs successfully on port `8081`.
3.  **Logging Divergence**: The logging mechanism in `code/app/backend/main.go` changes the handler entirely based on `Env`:
    *   **Development**: Uses `github.com/charmbracelet/log` (colorful, chat-like output).
    *   **Production**: Forces `slog.NewJSONHandler` with custom `ReplaceAttr` functions to standardize field names (`severity` instead of `level`, etc.).
4.  **Database Connection Disparity**: The connection mechanism differs drastically between local development (Podman/Docker/`pg_isready`) and production (gcloud/Cloud SQL Proxy/Direct connection attempt).

## 🏗️ Architecture & Data Flow

*   **Control Flow (Entry Point)**: All requests hit `code/app/backend/main.go:100` range. The `server.go` handlers coordinate calls between different `datastore` packages.
*   **Data Layer**: All persistence logic resides under `code/app/backend/datastore/`. Data models are centralized in `code/app/backend/models/`.
*   **Component Interaction**:
    *   **Discovery**: `code/app/backend/datastore/discovery` handles resource discovery endpoints.
    *   **Agent Integration**: The backend utilizes `code/services/researcher` capabilities (e.g., `/api/v1/discovery/mine`) which depend on credentials and context initialized during `make dev` or explicit calls.
    *   **Middleware**: Authentication/Logging concerns are handled in `code/app/backend/server/middleware.go`.

## 🧱 Conventions & Style

*   **Go Code Style**: *Crucial*: Never use double spacing in Go code. Keep logically joined sections of code together. Always run `goimports` upon writing Go code, especially before testing.
*   **CLI/Shell**: The `Makefile` employs variable substitution (`$$`) extensively, especially for target dependencies and conditional execution checks (`if ! podman info...`).
*   **Module Management**: For backend and services components, always run `go mod tidy` in the respective directories (`code/app/backend` and `code/services/researcher`).