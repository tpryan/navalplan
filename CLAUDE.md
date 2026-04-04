# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

NavalPlan is a sailing voyage planning web application. Users create multi-day itineraries, plan stops along a route, and leverage AI agents ("Virtual Harbourmaster") to research weather, tides, and navigation data. It also includes destination discovery and read-only sharing via secure links.

## Architecture Overview

The project has three major components:

**Frontend** (`code/app/frontend/`) — Vanilla JavaScript SPA using Vite 5. No framework. Google Maps for route visualization, Chart.js for data. Built output goes to `code/app/backend/static.min`. Proxies `/api` and `/auth` to the Go backend during dev.

**Backend** (`code/app/backend/`) — Go HTTP server (standard library + chi-style router). Entry point: `main.go`. Routes: `server/routes.go`. Handlers in `server/handlers/`. Data layer in `datastore/`. Models in `models/`. Uses PostgreSQL via `pgx/v5` and `sqlx`. Google OAuth for authentication.

**Researcher Agent** (`code/services/researcher/`) — Go microservice built on Google ADK. Orchestrates multiple AI sub-agents (researcher, guide, discovery, navigator) backed by Gemini models. Provides tools for weather, tides, sunrise, and places data. Communicates with the backend via A2A protocol.

**Database** — PostgreSQL 15. Local dev uses a Podman container. Migrations in `code/app/db/migrations/`.

## Commands

```bash
# Setup (creates .env, installs dependencies)
make setup

# Primary development (starts DB, backend, agent, and frontend concurrently)
make dev

# Run tests
make test               # all tests
make test-backend       # Go tests only
make test-frontend      # Jasmine tests only
make eval-all           # ADK agent evaluations

# Individual agent evaluations
make eval-researcher
make eval-guide
make eval-discovery
make eval-navigator VERBOSE=1   # add VERBOSE=1 for detailed output

# Build
make build              # frontend + backend
make build-js           # frontend only

# Database
make db-start           # start Podman PostgreSQL container
make db-stop
make db-reset           # stop, start, re-migrate
make migrate-up
make migrate-down       # rollback one migration
make migrate-create     # new migration file
make db-console         # interactive psql

# Dependency management
make tidy               # go mod tidy for all modules
make deps-backend       # update + vendor backend deps
make deps-researcher    # update + vendor agent deps
```

## Go Code Style

- Never use double spacing in Go code.
- Always run `goimports` after writing Go code, especially before testing.
- Keep logically joined sections of code together.
- Run `go mod tidy` in the respective module directory (`code/app/backend/` or `code/services/researcher/`) after dependency changes.

## Commit Messages

Use Conventional Commits format:
- `feat:`, `fix:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, `chore:`
- Optional scope: `fix(auth): ...`
- `BREAKING CHANGE:` footer for breaking API changes

## Key Operational Notes

**Environment Variables** — A `.env` file at the repo root is required for almost all operations. Copy `.env.example` and fill in credentials. Key vars: `NAVALPLAN_SYSTEM_KEY`, `NAVALPLAN_FRONTEND_MAPS_API_KEY`, `NAVALPLAN_BACKEND_MAPS_API_KEY`, `GEMINI_API_KEY`, Google OAuth secrets, and DB config.

**Logging** — Backend logging changes handler entirely based on `ENV`:
- Development: `charmbracelet/log` (colorful)
- Production: `slog.NewJSONHandler` with Cloud Logging field names (`severity` instead of `level`)

**Agent Evaluation** — The eval flow (`make eval-*`) is a multi-step shell orchestration that copies `.env` into `code/services/researcher/.env`, injects code into ADK Python files at runtime, and runs the agent on port 8081. It uses `google-adk` CLI.

**Database Connections** — Local uses Podman + `pg_isready`. Production uses Cloud SQL Auth Proxy. Never run `make migrate-prod` or `make deploy-sql` with local credentials; production path requires specific `gcloud` credentials.

**Production Deployment** — Cloud Run for both backend and agent. Cloud Build triggered via `make deploy-backend` and `make deploy-agent`. Secrets managed via Google Secret Manager.
