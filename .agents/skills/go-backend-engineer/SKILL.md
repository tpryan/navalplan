name: go-backend-engineer
description: Triggered when creating, modifying, optimizing, or refactoring Go (Golang) backend code, API handlers, data models, or service layers.

## Goal
To author clean, highly dense, mockable Go backend services utilizing standard libraries or minimal dependencies like chi, sqlx, and pgx/v5.

## Instructions
1. **Architecture Layering:** Adhere strictly to the defined layered architecture:
   * Define core domain definitions under `models/` using pure Go structs without external plumbing.
   * Encapsulate database query and mutation profiles under `datastore/`. Isolate actions behind an explicit interface (e.g., `Storer`).
   * Structure route registries in `server/routes.go` and inject clearing tiers (Public, Cookie Session, API Key, Admin).
   * Write endpoint logic inside `server/handlers/`. Inject data store interfaces rather than concrete types to maintain loose coupling.
2. **Post-Write Tooling Loop:** Immediately after updating any `.go` file, execute `goimports` via the sandbox shell to optimize imports. If third-party modules are introduced, run `go mod tidy` in the respective module context.

## Constraints
* **No Code Padding:** Never double-space Go source code. Group logical blocks together; allow at most one newline boundary between decoupled execution phases.
* **Interface Injection:** Handlers must never directly accept or instantiate concrete datastore types; mockability is a strict production requirement.
