name: database-lifecycle-manager
description: Triggered when defining or modifying database schemas, writing SQL queries, managing Podman/Docker containers, or running database migrations and Makefile targets.

## Goal
Safely manage database state, schema evolution, and containerized local lifecycles without ad-hoc mutations.

## Instructions
1. **Schema Evolution:** Store every database schema change as ordered, versioned `.up.sql` and `.down.sql` migration files under the dedicated migrations directory (e.g., `code/app/db/migrations/`).
2. **Containerized Infrastructure:** Ensure local database runs strictly inside Podman containers.
3. **Command Orchestration:** Utilize the centralized Makefile targets to interact with database states:
   * `make db-start` / `make db-stop` to manage containers.
   * `make db-reset` to wipe and recreate the database with clean migrations.
   * `make migrate-up` / `make migrate-down` to apply/rollback schema versions.

## Constraints
* **No Ad-Hoc Mutating:** Never execute ad-hoc schema modifications directly on a database instance. All changes must be codified via migrations.
