# Makefile
PROJECT_ID=your-project-id # Replace with actual ID later
REGION=us-central1
REPO_NAME=navalplan

# Database Local Config
DB_CONTAINER_NAME=navalplan-db
DB_NAME=navalplan
DB_USER=navalplan_user
DB_PASS=navalplan_pass
DB_PORT=5433

# Go
GO_FILES=$(shell find . -name '*.go')

.PHONY: run db-start db-stop db-reset test build-js clean-static run-frontend run-agent dev

# --- Development ---

# 1. RUN: Builds the frontend first, then runs Go serving that static folder
run: build-js
	@echo "Starting NavalPlan backend (Production Mode)..."
	# We assume .env is sourced or variables are set in your shell
	# For convenience, you can add a local .env loader here
	cd app/backend && go run -mod=vendor main.go --content=./static.min

# 2. BUILD: The master build command
build: build-js

# 2. RUN-BACKEND: Runs the Go backend without rebuilding JS (for dev)
run-backend:
	@echo "Starting NavalPlan backend (API Only)..."
	mkdir -p app/backend/static.min
	cd app/backend && go run -mod=vendor main.go --content=./static.min

# 3. CLEAN: Removes the old static files from the backend
clean-static:
	rm -rf app/backend/static.min

# 4. BUILD-JS: Installs deps and runs Vite Build
build-js: clean-static
	@echo "Building Frontend..."
	@if [ -z "$$NAVALPLAN_MB_TOKEN" ]; then \
		echo "Error: NAVALPLAN_MB_TOKEN is not set. Please set it in your environment or .env file."; \
		exit 1; \
	fi
	cd app/frontend && npm install
	cd app/frontend && npm run build

# --- Frontend ---

run-frontend:
	@echo "Starting NavalPlan frontend..."
	cd app/frontend && npm run dev

# --- Agent ---

run-agent:
	@echo "Starting NavalPlan Researcher Agent..."
	# Requires GEMINI_API_KEY to be set
	cd services/researcher && go run -mod=vendor main.go

# --- Combined Dev ---

dev: build-js
	@echo "Starting Backend, Frontend, and Agent..."
	@echo "Press Ctrl+C to stop all."
	@(trap 'kill 0' SIGINT; make run-backend & make run-agent & (sleep 3 && make run-frontend) & wait)

# --- Database (Podman/Docker) ---

db-start:
	@echo "Starting Database container ($(DB_CONTAINER_NAME))..."
	@podman run -d \
		--name $(DB_CONTAINER_NAME) \
		-p $(DB_PORT):5432 \
		-e POSTGRES_USER=$(DB_USER) \
		-e POSTGRES_PASSWORD=$(DB_PASS) \
		-e POSTGRES_DB=$(DB_NAME) \
		postgres:15-alpine || echo "Container likely already running"
	@echo "Waiting for DB to accept connections..."
	@sleep 3
	@make db-schema
	@make db-seed

db-stop:
	@echo "Stopping Database..."
	@podman stop $(DB_CONTAINER_NAME) || true
	@podman rm $(DB_CONTAINER_NAME) || true

db-reset: db-stop db-start
	@echo "Database has been reset and schema applied."

db-schema:
	@echo "Applying schema..."
	@podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) < app/db/schema.sql

db-seed:
	@echo "Seeding database..."
	@podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) < app/db/user.sql

db-console:
	@podman exec -it $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME)

# --- Testing ---

test:
	cd app/backend && go test ./... -cover
	cd services/researcher && go test ./... -cover

deps: deps-backend deps-researcher

deps-backend:
	cd app/backend && go mod tidy && go mod vendor

deps-researcher:
	cd services/researcher && go mod tidy && go mod vendor

# --- Deployment ---

deploy-agent:
	@echo "Deploying Agent..."
	gcloud builds submit --config cloudbuild-agent.yaml .

deploy-backend:
	@echo "Deploying Backend..."
	@if [ -z "$(AGENT_URL)" ]; then \
		echo "Warning: AGENT_URL is not set. Use 'make deploy-backend AGENT_URL=...'"; \
		gcloud builds submit --config cloudbuild.yaml .; \
	else \
		gcloud builds submit --config cloudbuild.yaml --substitutions=_AGENT_URL=$(AGENT_URL) .; \
	fi

# --- Cloud SQL ---

db-publish-prod:
	@echo "WARNING: This will DROP and RE-CREATE all tables in the PRODUCTION database 'navalplan' on instance 'wakelogdb'."
	@echo "Target Instance: wakelogdb"
	@echo "Target Database: navalplan"
	@echo -n "Are you sure? [y/N] "; \
	read ans; \
	if [ "$$ans" != "y" ]; then \
		echo "Aborting."; \
		exit 1; \
	fi
	@echo "Uploading schema to GCS..."
	gsutil cp app/db/pg-shortkey.sql gs://navalplan-logging-bucket/tmp/pg-shortkey.sql
	gsutil cp app/db/schema.sql gs://navalplan-logging-bucket/tmp/schema.sql
	@echo "Importing pg-shortkey.sql into Cloud SQL..."
	gcloud sql import sql wakelogdb gs://navalplan-logging-bucket/tmp/pg-shortkey.sql --database=navalplan --quiet
	@echo "Importing schema.sql into Cloud SQL..."
	gcloud sql import sql wakelogdb gs://navalplan-logging-bucket/tmp/schema.sql --database=navalplan --quiet
	@echo "Cleaning up GCS bucket..."
	gsutil rm gs://navalplan-logging-bucket/tmp/pg-shortkey.sql gs://navalplan-logging-bucket/tmp/schema.sql