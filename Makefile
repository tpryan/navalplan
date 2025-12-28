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

.PHONY: run db-start db-stop db-reset test

# --- Development ---

run:
	@echo "Starting NavalPlan backend..."
	# We assume .env is sourced or variables are set in your shell
	# For convenience, you can add a local .env loader here
	cd app/backend && go run main.go

# --- Frontend ---

run-frontend:
	@echo "Starting NavalPlan frontend..."
	cd app/frontend && npm run dev

# --- Combined Dev ---

dev:
	@echo "Starting Backend and Frontend..."
	@echo "Press Ctrl+C to stop both."
	@(trap 'kill 0' SIGINT; make run & make run-frontend & wait)

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

db-stop:
	@echo "Stopping Database..."
	@podman stop $(DB_CONTAINER_NAME) || true
	@podman rm $(DB_CONTAINER_NAME) || true

db-reset: db-stop db-start
	@echo "Database has been reset and schema applied."

db-schema:
	@echo "Applying schema..."
	@podman exec -i $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME) < app/db/schema.sql

db-console:
	@podman exec -it $(DB_CONTAINER_NAME) psql -U $(DB_USER) -d $(DB_NAME)

# --- Testing ---

test:
	cd app/backend && go test ./... -cover

deps:
	cd app/backend && go mod tidy && go mod vendor