---
name: makefile-orchestrator
description: Triggered when spinning up database environments, running application test sweeps, applying database schema migrations, or initiating agent evaluation pipelines.
---

# Makefile Orchestrator & Local Lifecycle Manager

## Goal
Exclusively leverage the root-level `Makefile` as the single point of coordination for developer environment workflows, local infrastructure runtime management, and automated test execution.

## Instructions
1. **Container Management:** Control local containerized database states via the Podman environment by utilizing `make db-start` and `make db-stop`. If a fresh, clear environment baseline is required, run `make db-reset`.
2. **Schema Control:** Apply and rollback versioned schema migrations strictly via the standard wrappers: `make migrate-up` or `make migrate-down`.
3. **Validation & Test Automation:** Initiate localized unit, component, or system-wide pipeline sweeps using `make test`. When performing deterministic multi-step agent quality tests, execute the appropriate specialized evaluation routine via `make eval-*`.

## Constraints
* **No Raw Shell Workarounds:** Do not manually construct long-form `podman`, `go test`, or raw `google-adk` shell commands if a pre-existing target recipe is mapped inside the project `Makefile`. Use the abstraction layers.
