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

# Migrations
MIGRATE_IMAGE=migrate/migrate
MIGRATE_PATH=app/db/migrations
DB_URL=postgres://$(DB_USER):$(DB_PASS)@localhost:$(DB_PORT)/$(DB_NAME)?sslmode=disable

# Production DB Config
PROD_INSTANCE=wakelogdb
PROD_DB_NAME=navalplan
PROD_DB_USER=navalplan_user
STORAGE_BUCKET=navallog-system
# PROD_DB_USER and PROD_DB_PASS should be set in your environment
# for the migrate-prod target.

# Go
GO_FILES=$(shell find . -name '*.go')

.PHONY: run db-start db-stop db-reset test build-js clean-static run-frontend run-agent dev migrate-up migrate-down migrate-create migrate-prod migrate-version migrate-force migrate-prod-version deploy-sql migrate-prod-gcs

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

setup:
	@echo "Setting up NavalPlan..."
	@if [ ! -f .env ]; then \
		echo "Creating .env from .env.example..."; \
		cp .env.example .env; \
		echo "WARNING: You must edit .env with your API keys (Google, Mapbox) before running!"; \
	else \
		echo ".env already exists. Skipping copy."; \
	fi
	@echo "Installing Go dependencies..."
	@make deps
	@echo "Installing Frontend dependencies..."
	@cd app/frontend && npm install
	@echo "Setup complete."
	@echo "1. Edit .env"
	@echo "2. Run 'make db-start' to start the database."
	@echo "3. Run 'make dev' to start the application."

setup-secrets:
	@chmod +x scripts/setup_secrets.sh
	@./scripts/setup_secrets.sh

setup-infra:
	@chmod +x scripts/setup_infra.sh
	@./scripts/setup_infra.sh

dev: build-js
	@echo "Starting Backend, Frontend, and Agent..."
	@echo "Press Ctrl+C to stop all."
	@(trap 'kill 0' SIGINT; make run-backend & make run-agent & (sleep 3 && make run-frontend) & wait)

dev-mine: 
	curl -X POST "http://localhost:8080/api/v1/discovery/mine?month=all" \
		-H "Cookie: navalplan_session=aFVMSIgIZL2sNFHbynQaiHR-f3SOFRV78DNbsZ2Be_U=" \
		-H "X-Requested-With: XMLHttpRequest"


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
	@make migrate-up
	@make db-seed

db-stop:
	@echo "Stopping Database..."
	@podman stop $(DB_CONTAINER_NAME) || true
	@podman rm $(DB_CONTAINER_NAME) || true

db-reset: db-stop db-start
	@echo "Database has been reset and migrations applied."

db-schema:
	@echo "Applying schema (DEPRECATED: use migrate-up)..."
	@podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) < app/db/schema.sql

db-seed:
	@echo "Seeding database..."
	@podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) < app/db/seed.sql

db-console:
	@podman exec -it $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME)

# --- Migrations ---

migrate-create:
	@echo "Creating a new migration..."
	@read -p "Migration name: " name; \
	podman run --rm -v $(PWD)/$(MIGRATE_PATH):/migrations:Z $(MIGRATE_IMAGE) create -ext sql -dir /migrations -seq $$name

migrate-up:
	@echo "Applying migrations..."
	@podman run --rm -v $(PWD)/$(MIGRATE_PATH):/migrations:Z --network host $(MIGRATE_IMAGE) -path=/migrations/ -database "$(DB_URL)" up

migrate-down:
	@echo "Rolling back migrations..."
	@podman run --rm -v $(PWD)/$(MIGRATE_PATH):/migrations:Z --network host $(MIGRATE_IMAGE) -path=/migrations/ -database "$(DB_URL)" down 1

migrate-version:
	@podman run --rm -v $(PWD)/$(MIGRATE_PATH):/migrations:Z --network host $(MIGRATE_IMAGE) -path=/migrations/ -database "$(DB_URL)" version

migrate-force:
	@read -p "Force version: " version; \
	podman run --rm -v $(PWD)/$(MIGRATE_PATH):/migrations:Z --network host $(MIGRATE_IMAGE) -path=/migrations/ -database "$(DB_URL)" force $$version

migrate-prod:
	@echo "Applying migrations to PRODUCTION ($(PROD_INSTANCE)) via Cloud SQL Auth Proxy..."
	@if [ -z "$(PROD_DB_USER)" ] || [ -z "$(PROD_DB_PASS)" ]; then \
		echo "Error: PROD_DB_USER and PROD_DB_PASS must be set."; \
		exit 1; \
	fi
	@echo -n "Are you sure you want to migrate PRODUCTION? [y/N] "; \
	read ans; \
	if [ "$$ans" != "y" ]; then \
		echo "Aborting."; \
		exit 1; \
	fi
	@# Start proxy in background (using port 5434 to avoid conflict with local DB)
	@gcloud sql auth-proxy --port 5434 $(PROD_INSTANCE) > /dev/null 2>&1 & PID=$$!; \
	echo "Waiting for Cloud SQL Auth Proxy (PID: $$PID)..."; \
	sleep 5; \
	podman run --rm -v $(PWD)/$(MIGRATE_PATH):/migrations:Z --network host $(MIGRATE_IMAGE) \
		-path=/migrations/ \
		-database "postgres://$(PROD_DB_USER):$(PROD_DB_PASS)@localhost:5434/$(PROD_DB_NAME)?sslmode=disable" up; \
	status=$$?; \
	kill $$PID; \
	exit $$status

migrate-prod-version:
	@echo "Checking PRODUCTION schema version..."
	@if [ -z "$(PROD_DB_USER)" ] || [ -z "$(PROD_DB_PASS)" ]; then \
		echo "Error: PROD_DB_USER and PROD_DB_PASS must be set."; \
		exit 1; \
	fi
	@gcloud sql auth-proxy --port 5434 $(PROD_INSTANCE) > /dev/null 2>&1 & PID=$$!; \
	sleep 5; \
	podman run --rm -v $(PWD)/$(MIGRATE_PATH):/migrations:Z --network host $(MIGRATE_IMAGE) \
		-path=/migrations/ \
		-database "postgres://$(PROD_DB_USER):$(PROD_DB_PASS)@localhost:5434/$(PROD_DB_NAME)?sslmode=disable" version; \
	kill $$PID

# --- Testing ---

test: test-backend test-frontend

test-backend:
	@echo "Running Backend Tests..."
	cd app/backend && go test ./... -cover
	cd services/researcher && go test ./... -cover

test-frontend:
	@echo "Running Frontend Tests..."
	cd app/frontend && npm test

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

deploy-scheduler:
	@echo "Deploying Cloud Scheduler Job..."
	@if [ -z "$(APP_URL)" ] || [ -z "$(SYSTEM_KEY)" ]; then \
		echo "Error: APP_URL and SYSTEM_KEY must be set."; \
		echo "Usage: make deploy-scheduler APP_URL=https://... SYSTEM_KEY=..."; \
		exit 1; \
	fi
	gcloud scheduler jobs create http mine-monthly-content \
		--schedule="0 0 1 * *" \
		--uri="$(APP_URL)/api/v1/discovery/mine?month=all" \
		--http-method=POST \
		--headers="Authorization=Bearer $(SYSTEM_KEY),X-Requested-With=CloudScheduler" \
		--location=$(REGION) \
		--description="Triggers discovery mining for all months" \
		--quiet || \
	echo "Job may already exist. Try updating it manually or ignore if intended."

# --- Cloud SQL ---

migrate-prod-gcs:
	@read -p "Enter migration version to apply (e.g., 000001): " version; \
	FILE=$$(ls $(MIGRATE_PATH)/$${version}_*.up.sql 2>/dev/null); \
	if [ -z "$$FILE" ]; then \
		echo "Error: Migration version $$version not found in $(MIGRATE_PATH)"; \
		exit 1; \
	fi; \
	FILENAME=$$(basename $$FILE); \
	echo "Applying $$FILENAME to PRODUCTION via GCS..."; \
	gsutil cp $$FILE gs://$(STORAGE_BUCKET)/$$FILENAME; \
	gcloud sql import sql $(PROD_INSTANCE) gs://$(STORAGE_BUCKET)/$$FILENAME --database=$(PROD_DB_NAME) --user=$(PROD_DB_USER) -q; \
	gsutil rm gs://$(STORAGE_BUCKET)/$$FILENAME

deploy-sql:
	@echo "Deploying SQL to PRODUCTION ($(PROD_INSTANCE)) via GCS Import..."
	@echo -n "Are you SURE? This is destructive. [y/N] "; \
	read ans; \
	if [ "$ans" != "y" ]; then \
		echo "Aborting."; \
		exit 1; \
	fi
	gsutil cp app/db/pg-shortkey.sql gs://$(STORAGE_BUCKET)/
	gsutil cp app/db/schema.sql gs://$(STORAGE_BUCKET)/
	gsutil cp app/db/seed.sql gs://$(STORAGE_BUCKET)/
	
	gcloud sql import sql $(PROD_INSTANCE) gs://$(STORAGE_BUCKET)/pg-shortkey.sql --database=$(PROD_DB_NAME) -q
	gcloud sql import sql $(PROD_INSTANCE) gs://$(STORAGE_BUCKET)/schema.sql --database=$(PROD_DB_NAME) -q
	gcloud sql import sql $(PROD_INSTANCE) gs://$(STORAGE_BUCKET)/seed.sql --database=$(PROD_DB_NAME) -q
	
	gsutil rm gs://$(STORAGE_BUCKET)/pg-shortkey.sql
	gsutil rm gs://$(STORAGE_BUCKET)/schema.sql
	gsutil rm gs://$(STORAGE_BUCKET)/seed.sql
db-publish-prod: deploy-sql
