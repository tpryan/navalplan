# Makefile
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

PROJECT_ID=your-project-id # Replace with actual ID later
REGION=us-central1
REPO_NAME=navalplan

# Database Local Config
DB_CONTAINER_NAME=navalplan-db
DB_NAME=${NAVALPLAN_DB_NAME}
DB_USER=${NAVALPLAN_DB_USER}
DB_PASS=${NAVALPLAN_DB_PASS}
DB_PORT=${NAVALPLAN_DB_PORT}

# Migrations
MIGRATE_IMAGE=migrate/migrate
MIGRATE_PATH=code/app/db/migrations
DB_URL=postgres://$(DB_USER):$(DB_PASS)@localhost:$(DB_PORT)/$(DB_NAME)?sslmode=disable

# Production DB Config
PROD_INSTANCE=wakelogdb
PROD_CONN_NAME=$(shell gcloud config get-value project 2>/dev/null):$(REGION):$(PROD_INSTANCE)
PROD_DB_NAME=navalplan
PROD_DB_USER=navalplan_user
STORAGE_BUCKET=navallog-system
# PROD_DB_USER and PROD_DB_PASS must be set in your environment for migrate-prod targets.

# Cloud SQL Auth Proxy binary (downloaded on demand to .bin/)
CLOUD_SQL_PROXY_VERSION=v2.14.1
CLOUD_SQL_PROXY=.bin/cloud-sql-proxy

# golang-migrate binary for prod targets — avoids Podman VM networking issues
MIGRATE_BIN_VERSION=v4.18.1
MIGRATE_BIN=.bin/migrate

# Go
GO_FILES=$(shell find . -name '*.go')

# ADK CLI — prefer venv if present
ADK?=$(shell [ -f ./venv/bin/adk ] && echo ./venv/bin/adk || echo adk)

.PHONY: run db-start db-stop db-reset test eval eval-all eval-agent eval-harbourmaster eval-pilot eval-commodore eval-specialist build-js clean-static run-frontend run-agent dev migrate-up migrate-down migrate-create migrate-prod migrate-version migrate-force migrate-prod-version migrate-prod-force deploy-sql migrate-prod-gcs tidy setup-adk install-cloud-sql-proxy install-migrate

# --- Development ---

# 1. RUN: Builds the frontend first, then runs Go serving that static folder
run: build-js
	@echo "Starting NavalPlan backend (Production Mode)..."
	# Variables are automatically loaded from .env
	cd code/app/backend && NAVALPLAN_CONTENT_DIR=./static.min go run -mod=vendor .
# 2. BUILD: The master build command
build: build-js

# 2. RUN-BACKEND: Runs the Go backend without rebuilding JS (for dev)
run-backend:
	@echo "Starting NavalPlan backend (API Only)..."
	mkdir -p code/app/backend/static.min
	cd code/app/backend && NAVALPLAN_CONTENT_DIR=./static.min go run -mod=vendor .

# 3. CLEAN: Removes the old static files from the backend
clean-static:
	rm -rf code/app/backend/static.min

# 4. BUILD-JS: Installs deps and runs Vite Build
build-js: clean-static
	@echo "Building Frontend..."
	@if [ -z "$$NAVALPLAN_FRONTEND_MAPS_API_KEY" ]; then \
		echo "Error: NAVALPLAN_FRONTEND_MAPS_API_KEY is not set. Please set it in your environment or .env file."; \
		exit 1; \
	fi
	cd code/app/frontend && npm install
	cd code/app/frontend && npm run build

# --- Frontend ---

run-frontend:
	@echo "Starting NavalPlan frontend..."
	cd code/app/frontend && npm run dev

# --- Agent ---

run-agent:
	@echo "Starting NavalPlan Researcher Agent..."
	# Requires GEMINI_API_KEY to be set
	cd code/services/researcher && go run -mod=vendor .

setup-adk:
	@echo "Setting up ADK..."
	@if [ -d venv ]; then \
		./venv/bin/pip install "google-adk[a2a,eval]"; \
	else \
		pip install "google-adk[a2a,eval]" || echo "Warning: Could not install google-adk[a2a,eval] globally. Please ensure it is installed."; \
	fi

# --- Combined Dev ---

setup:
	@echo "Setting up NavalPlan..."
	@if [ ! -f .env ]; then \
		echo "Creating .env from .env.example..."; \
		cp .env.example .env; \
		echo "WARNING: You must edit .env with your API keys (Google, Google Maps) before running!"; \
	else \
		echo ".env already exists. Skipping copy."; \
	fi
	@echo "Installing Go dependencies..."
	@make deps
	@echo "Installing Frontend dependencies..."
	@cd code/app/frontend && npm install
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

dev: db-start build-js
	@echo "Starting Backend, Frontend, and Agent..."
	@echo "Press Ctrl+C to stop all."
	@(trap 'kill 0' SIGINT; make run-backend & make run-agent & (sleep 3 && make run-frontend) & wait)

dev-mine: 
	curl -X POST "http://localhost:8080/api/v1/discovery/mine?month=all" \
		-H "Authorization: Bearer ${NAVALPLAN_SYSTEM_KEY}" \
		-H "X-Requested-With: XMLHttpRequest"


# --- Database (Podman/Docker) ---

db-start:
	@echo "Starting Database container ($(DB_CONTAINER_NAME))..."
	@if ! podman info >/dev/null 2>&1; then \
		echo "Podman is not running. Attempting to start Podman machine..."; \
		podman machine start || (echo "Error: Failed to start Podman machine. Please start it manually." && exit 1); \
	fi
	@if podman inspect $(DB_CONTAINER_NAME) >/dev/null 2>&1; then \
		echo "Container $(DB_CONTAINER_NAME) exists. Starting..."; \
		podman start $(DB_CONTAINER_NAME); \
	else \
		echo "Creating and starting container $(DB_CONTAINER_NAME)..."; \
		podman run -d \
			--name $(DB_CONTAINER_NAME) \
			-p $(DB_PORT):5432 \
			-e POSTGRES_USER=$(DB_USER) \
			-e POSTGRES_PASSWORD=$(DB_PASS) \
			-e POSTGRES_DB=$(DB_NAME) \
			postgres:15-alpine; \
	fi
	@echo "Waiting for DB to accept connections..."
	@timeout=30; \
	until podman exec $(DB_CONTAINER_NAME) pg_isready -U $(DB_USER) >/dev/null 2>&1 || [ $$timeout -le 0 ]; do \
		echo "Waiting for DB... ($$timeout)"; \
		sleep 1; \
		timeout=$$((timeout-1)); \
	done; \
	if [ $$timeout -le 0 ]; then echo "Timed out waiting for DB"; exit 1; fi
	@make migrate-up
	@make db-seed || echo "Seeding skipped (likely already seeded)"

db-stop:
	@echo "Stopping Database..."
	@podman stop $(DB_CONTAINER_NAME) || true
	@podman rm $(DB_CONTAINER_NAME) || true

db-reset: db-stop db-start
	@echo "Database has been reset and migrations applied."

db-schema:
	@echo "Applying schema (DEPRECATED: use migrate-up)..."
	@podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) < code/app/db/schema.sql

db-seed:
	@echo "Seeding database..."
	@podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) < code/app/db/seed.sql

db-console:
	@podman exec -it $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME)

add-admin:
	@if [ -z "$(EMAIL)" ]; then echo "Usage: make add-admin EMAIL=user@example.com"; exit 1; fi
	@echo "Ensuring admin status for $(EMAIL)..."
	@EXISTING_ID=$$(podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) -t -c "SELECT id FROM person WHERE email = '$(EMAIL)';" | xargs); \
	if [ -n "$$EXISTING_ID" ]; then \
		echo "User exists (ID: $$EXISTING_ID). Promoting to admin..."; \
		podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) -c "UPDATE person SET is_admin = TRUE WHERE id = $$EXISTING_ID;"; \
	else \
		echo "User does not exist. Creating admin invitation..."; \
		podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) -c "INSERT INTO invitation (email, is_admin) VALUES ('$(EMAIL)', TRUE) ON CONFLICT (email) DO UPDATE SET is_admin = TRUE;"; \
	fi
	@echo "Done."
	
add-admin-prod: .bin/cloud-sql-proxy
	@if [ -z "$(EMAIL)" ]; then echo "Usage: make add-admin-prod EMAIL=user@example.com"; exit 1; fi
	@set -a; [ -f .env ] && . ./.env; set +a; \
	if [ -z "$$PROD_DB_USER" ] || [ -z "$$PROD_DB_PASS" ]; then \
		echo "Error: PROD_DB_USER and PROD_DB_PASS must be set in .env or environment."; \
		exit 1; \
	fi; \
	echo "Ensuring admin status for $(EMAIL) in PRODUCTION..."; \
	PROXY_LOG=$$(mktemp); \
	$(CLOUD_SQL_PROXY) --port 5434 $(PROD_CONN_NAME) > $$PROXY_LOG 2>&1 & PID=$$!; \
	echo "Waiting for Cloud SQL Auth Proxy (PID: $$PID)..."; \
	for i in $$(seq 1 15); do \
		kill -0 $$PID 2>/dev/null || { echo "Proxy crashed. Output:"; cat $$PROXY_LOG; rm -f $$PROXY_LOG; exit 1; }; \
		bash -c "echo > /dev/tcp/127.0.0.1/5434" 2>/dev/null && { echo "Proxy ready."; break; }; \
		[ $$i -eq 15 ] && { echo "Proxy not ready after 15s. Output:"; cat $$PROXY_LOG; kill $$PID; rm -f $$PROXY_LOG; exit 1; }; \
		sleep 1; \
	done; \
	rm -f $$PROXY_LOG; \
	PGPASSWORD=$$PROD_DB_PASS psql -h 127.0.0.1 -p 5434 -U $$PROD_DB_USER -d $(PROD_DB_NAME) -c "UPDATE person SET is_admin = TRUE WHERE email = '$(EMAIL)';" | grep "UPDATE 1" || \
	PGPASSWORD=$$PROD_DB_PASS psql -h 127.0.0.1 -p 5434 -U $$PROD_DB_USER -d $(PROD_DB_NAME) -c "INSERT INTO invitation (email, is_admin) VALUES ('$(EMAIL)', TRUE) ON CONFLICT (email) DO UPDATE SET is_admin = TRUE;"; \
	kill $$PID; \
	echo "Done."

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

migrate-prod-force: .bin/cloud-sql-proxy .bin/migrate
	@echo "Force-setting PRODUCTION schema version (use this to mark existing schema as migrated)..."
	@set -a; [ -f .env ] && . ./.env; set +a; \
	if [ -z "$$PROD_DB_USER" ] || [ -z "$$PROD_DB_PASS" ]; then \
		echo "Error: PROD_DB_USER and PROD_DB_PASS must be set in .env or environment."; \
		exit 1; \
	fi; \
	read -p "Force version: " version; \
	PROXY_LOG=$$(mktemp); \
	$(CLOUD_SQL_PROXY) --port 5434 $(PROD_CONN_NAME) > $$PROXY_LOG 2>&1 & PID=$$!; \
	echo "Waiting for Cloud SQL Auth Proxy (PID: $$PID)..."; \
	for i in $$(seq 1 15); do \
		kill -0 $$PID 2>/dev/null || { echo "Proxy crashed. Output:"; cat $$PROXY_LOG; rm -f $$PROXY_LOG; exit 1; }; \
		bash -c "echo > /dev/tcp/127.0.0.1/5434" 2>/dev/null && { echo "Proxy ready."; break; }; \
		[ $$i -eq 15 ] && { echo "Proxy not ready after 15s. Output:"; cat $$PROXY_LOG; kill $$PID; rm -f $$PROXY_LOG; exit 1; }; \
		sleep 1; \
	done; \
	rm -f $$PROXY_LOG; \
	$(MIGRATE_BIN) -path=$(MIGRATE_PATH) \
		-database "postgres://$$PROD_DB_USER:$$PROD_DB_PASS@127.0.0.1:5434/$(PROD_DB_NAME)?sslmode=disable" force $$version; \
	kill $$PID

# Downloads the Cloud SQL Auth Proxy binary for the current OS/arch if not already present.
# Override with: CLOUD_SQL_PROXY=/usr/local/bin/cloud-sql-proxy make migrate-prod
.bin/cloud-sql-proxy:
	@mkdir -p .bin
	@echo "Downloading Cloud SQL Auth Proxy $(CLOUD_SQL_PROXY_VERSION)..."
	@OS=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
	ARCH=$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/'); \
	curl -fsSL -o .bin/cloud-sql-proxy \
		"https://storage.googleapis.com/cloud-sql-connectors/cloud-sql-proxy/$(CLOUD_SQL_PROXY_VERSION)/cloud-sql-proxy.$${OS}.$${ARCH}"
	@chmod +x .bin/cloud-sql-proxy

install-cloud-sql-proxy: .bin/cloud-sql-proxy

.bin/migrate:
	@mkdir -p .bin
	@echo "Downloading golang-migrate $(MIGRATE_BIN_VERSION)..."
	@OS=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
	ARCH=$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/'); \
	curl -fsSL -o /tmp/migrate.tar.gz \
		"https://github.com/golang-migrate/migrate/releases/download/$(MIGRATE_BIN_VERSION)/migrate.$${OS}-$${ARCH}.tar.gz"; \
	tar -xzf /tmp/migrate.tar.gz -C .bin migrate; \
	rm /tmp/migrate.tar.gz
	@chmod +x .bin/migrate

install-migrate: .bin/migrate

migrate-prod: .bin/cloud-sql-proxy .bin/migrate
	@echo "Applying migrations to PRODUCTION ($(PROD_INSTANCE)) via Cloud SQL Auth Proxy..."
	@set -a; [ -f .env ] && . ./.env; set +a; \
	if [ -z "$$PROD_DB_USER" ] || [ -z "$$PROD_DB_PASS" ]; then \
		echo "Error: PROD_DB_USER and PROD_DB_PASS must be set in .env or environment."; \
		exit 1; \
	fi; \
	echo -n "Are you sure you want to migrate PRODUCTION? [y/N] "; \
	read ans; \
	if [ "$$ans" != "y" ]; then \
		echo "Aborting."; \
		exit 1; \
	fi; \
	PROXY_LOG=$$(mktemp); \
	$(CLOUD_SQL_PROXY) --port 5434 $(PROD_CONN_NAME) > $$PROXY_LOG 2>&1 & PID=$$!; \
	echo "Waiting for Cloud SQL Auth Proxy (PID: $$PID)..."; \
	for i in $$(seq 1 15); do \
		kill -0 $$PID 2>/dev/null || { echo "Proxy crashed. Output:"; cat $$PROXY_LOG; rm -f $$PROXY_LOG; exit 1; }; \
		bash -c "echo > /dev/tcp/127.0.0.1/5434" 2>/dev/null && { echo "Proxy ready."; break; }; \
		[ $$i -eq 15 ] && { echo "Proxy not ready after 15s. Output:"; cat $$PROXY_LOG; kill $$PID; rm -f $$PROXY_LOG; exit 1; }; \
		sleep 1; \
	done; \
	rm -f $$PROXY_LOG; \
	$(MIGRATE_BIN) -path=$(MIGRATE_PATH) \
		-database "postgres://$$PROD_DB_USER:$$PROD_DB_PASS@127.0.0.1:5434/$(PROD_DB_NAME)?sslmode=disable" up; \
	status=$$?; \
	kill $$PID; \
	exit $$status

migrate-prod-version: .bin/cloud-sql-proxy .bin/migrate
	@echo "Checking PRODUCTION schema version..."
	@set -a; [ -f .env ] && . ./.env; set +a; \
	if [ -z "$$PROD_DB_USER" ] || [ -z "$$PROD_DB_PASS" ]; then \
		echo "Error: PROD_DB_USER and PROD_DB_PASS must be set in .env or environment."; \
		exit 1; \
	fi; \
	PROXY_LOG=$$(mktemp); \
	$(CLOUD_SQL_PROXY) --port 5434 $(PROD_CONN_NAME) > $$PROXY_LOG 2>&1 & PID=$$!; \
	echo "Waiting for Cloud SQL Auth Proxy (PID: $$PID)..."; \
	for i in $$(seq 1 15); do \
		kill -0 $$PID 2>/dev/null || { echo "Proxy crashed. Output:"; cat $$PROXY_LOG; rm -f $$PROXY_LOG; exit 1; }; \
		bash -c "echo > /dev/tcp/127.0.0.1/5434" 2>/dev/null && { echo "Proxy ready."; break; }; \
		[ $$i -eq 15 ] && { echo "Proxy not ready after 15s. Output:"; cat $$PROXY_LOG; kill $$PID; rm -f $$PROXY_LOG; exit 1; }; \
		sleep 1; \
	done; \
	rm -f $$PROXY_LOG; \
	$(MIGRATE_BIN) -path=$(MIGRATE_PATH) \
		-database "postgres://$$PROD_DB_USER:$$PROD_DB_PASS@127.0.0.1:5434/$(PROD_DB_NAME)?sslmode=disable" version; \
	kill $$PID

# --- Testing ---

test: test-backend test-frontend eval-all

test-backend:
	@echo "Running Backend Tests..."
	cd code/app/backend && go test ./... -cover
	cd code/services/researcher && go test ./... -cover

test-frontend:
	@echo "Running Frontend Tests..."
	cd code/app/frontend && npm test

# --- Evaluation ---

eval: eval-all

eval-harbourmaster:
	@$(MAKE) eval-agent AGENT=harbourmaster

eval-pilot:
	@$(MAKE) eval-agent AGENT=pilot

eval-commodore:
	@$(MAKE) eval-agent AGENT=commodore

eval-specialist:
	@$(MAKE) eval-agent AGENT=specialist

eval-agent:
	@if [ "$(VERBOSE)" != "1" ]; then \
		echo "Evaluating NavalPlan Agent ($$AGENT)... (Set VERBOSE=1 for full output)"; \
	else \
		echo "Starting NavalPlan Agent ($$AGENT) for evaluation..."; \
	fi
	@mkdir -p .adk
	@ln -sf $$(pwd)/.env code/services/researcher/.env
	@echo "from . import agent" > code/services/researcher/__init__.py
	@if [ "$(VERBOSE)" != "1" ]; then \
		(cd code/services/researcher && go run -mod=vendor .) > /dev/null 2>&1 & echo $$! > agent.pid; \
	else \
		(cd code/services/researcher && go run -mod=vendor .) & echo $$! > agent.pid; \
	fi
	@sleep 15
	@if ! lsof -i :8081 > /dev/null; then \
		echo "Error: Agents failed to start on port 8081"; \
		kill $$(cat agent.pid) 2>/dev/null || true; \
		rm agent.pid; \
		exit 1; \
	fi; \
	if [ "$$AGENT" = "harbourmaster" ]; then CARD="http://localhost:8081/invoke/agent-card.json"; else CARD="http://localhost:8081/invoke/$$AGENT/agent-card.json"; fi; \
	echo "from google.adk.agents.remote_a2a_agent import RemoteA2aAgent" > code/services/researcher/agent.py; \
	echo "agent = RemoteA2aAgent(name='$${AGENT}_agent', agent_card='$$CARD', use_legacy=False)" >> code/services/researcher/agent.py; \
	echo "root_agent = agent" >> code/services/researcher/agent.py; \
	if [ "$(VERBOSE)" != "1" ]; then \
		PYTHONWARNINGS=ignore $(ADK) eval code/services/researcher code/services/researcher/eval/$$AGENT/$$AGENT.test.json --config_file_path=code/services/researcher/eval/$$AGENT/test_config.json 2>/dev/null | grep -A 10 "Eval Run Summary"; \
	else \
		$(ADK) eval code/services/researcher code/services/researcher/eval/$$AGENT/$$AGENT.test.json --config_file_path=code/services/researcher/eval/$$AGENT/test_config.json --print_detailed_results; \
	fi; \
	EXIT_CODE=$$?; \
	lsof -ti :8081 | xargs kill -9 2>/dev/null || true; \
	rm -f agent.pid; \
	rm -f code/services/researcher/.env; \
	rm -f code/services/researcher/__init__.py code/services/researcher/agent.py; \
	exit $$EXIT_CODE

eval-all:
	@echo "Starting all NavalPlan Agents for evaluation..."
	@EXIT_CODE=0; \
	for agent in harbourmaster pilot commodore specialist; do \
		$(MAKE) eval-agent AGENT=$$agent; \
		CUR_EXIT=$$?; \
		if [ $$CUR_EXIT -ne 0 ]; then EXIT_CODE=$$CUR_EXIT; fi; \
	done; \
	exit $$EXIT_CODE

deps: deps-backend deps-researcher

deps-backend:
	cd code/app/backend && go mod tidy && go mod vendor

deps-researcher:
	cd code/services/researcher && go mod tidy && go mod vendor

tidy: tidy-backend tidy-researcher

tidy-backend:
	cd code/app/backend && go mod tidy && go mod vendor

tidy-researcher:
	cd code/services/researcher && go mod tidy && go mod vendor

# --- Deployment ---

deploy-agent:
	@echo "Deploying Agent..."
	gcloud builds submit --config .cloudbuild/cloudbuild-agent.yaml --substitutions=SHORT_SHA=$(shell git rev-parse --short HEAD) .

deploy-agent-runtime:
	@echo "Deploying Researcher Agent to Vertex AI Agent Runtime..."
	cd code/services/researcher && agents-cli deploy \
		--project $(shell gcloud config get-value project) \
		--region $(REGION) \
		--service-name navalplan-researcher \
		--no-confirm-project

deploy-backend:
	@echo "Deploying Backend..."
	gcloud builds submit --config .cloudbuild/cloudbuild.yaml .

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
	gcloud storage cp $$FILE gs://$(STORAGE_BUCKET)/$$FILENAME; \
	gcloud sql import sql $(PROD_INSTANCE) gs://$(STORAGE_BUCKET)/$$FILENAME --database=$(PROD_DB_NAME) --user=$(PROD_DB_USER) -q; \
	gcloud storage rm gs://$(STORAGE_BUCKET)/$$FILENAME

deploy-sql:
	@echo "Deploying SQL to PRODUCTION ($(PROD_INSTANCE)) via GCS Import..."
	@echo -n "Are you SURE? This is destructive. [y/N] "; \
	read ans; \
	if [ "$ans" != "y" ]; then \
		echo "Aborting."; \
		exit 1; \
	fi
	gcloud storage cp code/app/db/pg-shortkey.sql gs://$(STORAGE_BUCKET)/
	gcloud storage cp code/app/db/schema.sql gs://$(STORAGE_BUCKET)/
	gcloud storage cp code/app/db/seed.sql gs://$(STORAGE_BUCKET)/
	
	gcloud sql import sql $(PROD_INSTANCE) gs://$(STORAGE_BUCKET)/pg-shortkey.sql --database=$(PROD_DB_NAME) -q
	gcloud sql import sql $(PROD_INSTANCE) gs://$(STORAGE_BUCKET)/schema.sql --database=$(PROD_DB_NAME) -q
	gcloud sql import sql $(PROD_INSTANCE) gs://$(STORAGE_BUCKET)/seed.sql --database=$(PROD_DB_NAME) -q
	
	gcloud storage rm gs://$(STORAGE_BUCKET)/pg-shortkey.sql
	gcloud storage rm gs://$(STORAGE_BUCKET)/schema.sql
	gcloud storage rm gs://$(STORAGE_BUCKET)/seed.sql
db-publish-prod: deploy-sql

# --- Agent Registry ---

NAVALPLAN_RESEARCHER_URL?=$(shell gcloud run services describe navalplan-researcher --region $(REGION) --format='value(status.url)' 2>/dev/null)

register-registry: register-researcher register-mcp register-agents-all

register-researcher:
	@echo "Registering Base Researcher Service in Agent Registry..."
	@if [ -z "$(NAVALPLAN_RESEARCHER_URL)" ]; then \
		echo "Error: Could not determine researcher service URL. Is it deployed?"; \
		exit 1; \
	fi
	gcloud alpha agent-registry services create navalplan-researcher \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Researcher Service" \
		--description "The base service for all NavalPlan agents." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL),protocolBinding=http-json" || \
	gcloud alpha agent-registry services update navalplan-researcher \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Researcher Service" \
		--description "The base service for all NavalPlan agents." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL),protocolBinding=http-json"

register-mcp:
	@echo "Registering MCP Service in Agent Registry..."
	@if [ -z "$(NAVALPLAN_RESEARCHER_URL)" ]; then \
		echo "Error: Could not determine researcher service URL. Is it deployed?"; \
		exit 1; \
	fi
	gcloud alpha agent-registry services create navalplan-mcp \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan MCP Tools" \
		--description "Provides nautical tools for tides, weather, and safety alerts." \
		--mcp-server-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/mcp/tools,protocolBinding=jsonrpc" || \
	gcloud alpha agent-registry services update navalplan-mcp \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan MCP Tools" \
		--description "Provides nautical tools for tides, weather, and safety alerts." \
		--mcp-server-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/mcp/tools,protocolBinding=jsonrpc"

register-agents-all: register-harbourmaster register-pilot register-commodore register-specialist register-lookout

register-harbourmaster:
	@echo "Registering Harbourmaster Agent..."
	gcloud alpha agent-registry services create navalplan-harbourmaster \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Harbourmaster" \
		--description "Main coordinator for nautical operations." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/harbourmaster,protocolBinding=http-json" || \
	gcloud alpha agent-registry services update navalplan-harbourmaster \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Harbourmaster" \
		--description "Main coordinator for nautical operations." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/harbourmaster,protocolBinding=http-json"

register-pilot:
	@echo "Registering Pilot Agent..."
	gcloud alpha agent-registry services create navalplan-pilot \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Pilot" \
		--description "Expert in local navigation and nautical rules." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/pilot,protocolBinding=http-json" || \
	gcloud alpha agent-registry services update navalplan-pilot \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Pilot" \
		--description "Expert in local navigation and nautical rules." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/pilot,protocolBinding=http-json"

register-commodore:
	@echo "Registering Commodore Agent..."
	gcloud alpha agent-registry services create navalplan-commodore \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Commodore" \
		--description "High-level strategic planning and fleet management." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/commodore,protocolBinding=http-json" || \
	gcloud alpha agent-registry services update navalplan-commodore \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Commodore" \
		--description "High-level strategic planning and fleet management." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/commodore,protocolBinding=http-json"

register-specialist:
	@echo "Registering Specialist Agent..."
	gcloud alpha agent-registry services create navalplan-specialist \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Specialist" \
		--description "Technical specialist for specific maritime tasks." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/specialist,protocolBinding=http-json" || \
	gcloud alpha agent-registry services update navalplan-specialist \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Specialist" \
		--description "Technical specialist for specific maritime tasks." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/specialist,protocolBinding=http-json"

register-lookout:
	@echo "Registering Lookout Agent..."
	gcloud alpha agent-registry services create navalplan-lookout \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Lookout" \
		--description "Monitoring and alerting for maritime safety." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/lookout,protocolBinding=http-json" || \
	gcloud alpha agent-registry services update navalplan-lookout \
		--project $(shell gcloud config get-value project) \
		--location global \
		--display-name "NavalPlan Lookout" \
		--description "Monitoring and alerting for maritime safety." \
		--agent-spec-type=no-spec \
		--interfaces "url=$(NAVALPLAN_RESEARCHER_URL)/invoke/lookout,protocolBinding=http-json"

register-agents:
	@echo "Registering Agents in Gemini Enterprise..."
	@if [ -z "$(GEMINI_ENTERPRISE_APP_ID)" ]; then \
		echo "Error: GEMINI_ENTERPRISE_APP_ID is not set."; \
		exit 1; \
	fi
	# Use agents-cli publish for agent registration if available
	@if [ -f "./venv/bin/agents-cli" ]; then \
		./venv/bin/agents-cli publish gemini-enterprise \
			--gemini-enterprise-app-id $(GEMINI_ENTERPRISE_APP_ID) \
			--interactive; \
	else \
		echo "Warning: agents-cli not found. Skipping Gemini Enterprise publishing."; \
	fi
