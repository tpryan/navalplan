name: environment-aware-logger
description: Triggered when writing logs, managing logging libraries, handling application diagnostic output, or configuring observability frameworks.

## Goal
Implement a robust logging mechanism that automatically pivots formatting between human-friendly dev outputs and structured cloud logging.

## Instructions
1. **Dev Logging (ENV=development):** Route logs into `charmbracelet/log` for colorized, human-readable terminal output.
2. **Production Logging (ENV=production):** Force logs to output structured JSON conforming to Google Cloud Logging specifications.

## Constraints
* **GCP Severity Matching:** When printing JSON logs in production, map severity keys precisely. Use `severity` instead of `level` and `message` instead of `msg`.
