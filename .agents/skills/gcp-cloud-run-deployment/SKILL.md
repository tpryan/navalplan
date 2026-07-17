---
name: gcp-cloud-run-deployment
description: Triggered when editing continuous integration setups, modifying Cloud Build manifests, writing Dockerfiles, defining GCP infrastructure patterns, or configuring environment secrets/databases.
---

# GCP Cloud Run Deployment & Build Architecture

## Goal
Streamline and automate secure, containerized serverless deployments to Google Cloud Platform (GCP) triggered seamlessly by GitHub version control events.

## Instructions
1. **Target Architecture & Workloads:** 
   * Deploy the core Go backend servers and standalone ADK AI microservices to **GCP Cloud Run** for stateless scaling.
   * Restrict **Cloud Functions** to lightweight, event-driven background routines (such as responding to Google Cloud Storage bucket object arrivals).
2. **Strict Directory Isolation:** 
   * All deployment orchestration and containerization assets must live inside the **`.cloudbuild/`** root folder. 
   * This includes `cloudbuild.yaml` pipelines and any service-specific `Dockerfile` manifests (e.g., `.cloudbuild/backend.Dockerfile`, `.cloudbuild/agent.Dockerfile`).
3. **GitHub-Driven Automation Triggers:** 
   * Build pipelines are entirely event-driven, responding to GitHub check-in workflows via **Cloud Build Triggers**.
   * Configure triggers to automatically intercept pushes to target branch patterns (e.g., `main` or release tags) to build, test, and release the containers to the Artifact Registry before updating the Cloud Run services.
4. **Secret Security & Handshakes:** 
   * Inject application environment variables dynamically at runtime by referencing maps inside **Google Secret Manager**. 
   * Production SQL access must bypass public network vectors entirely, routing strictly through secure Cloud SQL Auth Proxy socket endpoints mapped via the **`NAVALLOG_DB_SOCKET`** environment parameter.

## Constraints
* **No Plaintext Credentials:** Never commit API keys, database credentials, or private access tokens into repository configuration files, Dockerfiles, or plaintext environment maps.
* **Root-Level Cleanliness:** Keep the root workspace free of deployment noise. Do not place standalone Dockerfiles or top-level `cloudbuild.yaml` files outside the designated `.cloudbuild/` folder.