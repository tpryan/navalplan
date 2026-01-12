# NavalPlan Deployment Guide

This guide covers the process for setting up the Google Cloud infrastructure and deploying the NavalPlan application.

## Prerequisites

1.  **Google Cloud Project**: Create a project in the [Google Cloud Console](https://console.cloud.google.com/).
2.  **gcloud CLI**: [Installed and authenticated](https://cloud.google.com/sdk/docs/install).
    ```bash
    gcloud auth login
    gcloud config set project YOUR_PROJECT_ID
    ```
3.  **Local Tools**: Ensure `make`, `go`, and `npm` are installed (see root `README.md`).

---

## 1. Infrastructure Provisioning

NavalPlan uses several Google Cloud services: Cloud Run, Cloud SQL (Postgres), Secret Manager, Artifact Registry, and Cloud Storage.

Run the automated infrastructure setup:

```bash
make setup-infra
```

**What this script does:**
*   Enables all necessary Google Cloud APIs.
*   Creates an Artifact Registry named `navalplan-repo`.
*   Creates two Storage Buckets:
    *   `gs://<project-id>-logging`: For build logs.
    *   `gs://<project-id>-system`: For database migrations and exports.
*   Creates a Cloud SQL instance named `wakelogdb` (Postgres 15).
*   Creates the `navalplan` database and `navalplan_user`.

---

## 2. Secrets Configuration

The application retrieves sensitive configuration from Secret Manager.

```bash
make setup-secrets
```

The script will prompt you for the following values:
*   `NAVALPLAN_FRONTEND_MAPS_API_KEY`: Google Maps API key for the frontend.
*   `NAVALPLAN_BACKEND_MAPS_API_KEY`: Google Maps API key for the backend (optional, if separate).
*   `NAVALPLAN_MAP_ID`: Map ID for custom styling.
*   `NAVALPLAN_OA_CLIENT`: Google OAuth Client ID.
*   `NAVALPLAN_OA_SECRET`: Google OAuth Client Secret.
*   `NAVALPLAN_SYSTEM_KEY`: A secure random string for internal API calls.
*   `NAVALPLAN_DB_PASS`: The password you set during `setup-infra`.

---

## 3. Configuration Updates

After provisioning, update the following files in your repository to match your project resources:

1.  **`cloudbuild.yaml` & `cloudbuild-agent.yaml`**:
    Update the `logsBucket` field:
    ```yaml
    logsBucket: gs://YOUR_PROJECT_ID-logging
    ```
2.  **`Makefile`**:
    Update the `STORAGE_BUCKET` variable:
    ```makefile
    STORAGE_BUCKET=YOUR_PROJECT_ID-system
    ```

---

## 4. Database Initialization

To apply the schema and seed data to your production database:

```bash
make deploy-sql
```

*Note: This script uploads the SQL files to your system bucket and imports them into Cloud SQL.*

---

## 5. Deployment

Once infrastructure and secrets are ready, deploy the services using Cloud Build.

### Deploy the Researcher Agent
The agent is a private service that handles AI-powered research.

```bash
make deploy-agent
```

### Deploy the Backend
The backend serves the frontend and handles the main application logic.

```bash
make deploy-backend
```

### Deploy the Scheduler
This sets up a monthly job to trigger discovery mining.

```bash
make deploy-scheduler APP_URL=https://your-backend-url.run.app SYSTEM_KEY=your-system-key
```

---

## Troubleshooting

### Permissions
Ensure the default Cloud Build service account (`PROJECT_NUMBER@cloudbuild.gserviceaccount.com`) has the following roles:
*   `Cloud Run Admin`
*   `Service Account User`
*   `Secret Manager Secret Accessor`
*   `Cloud SQL Client`

### Database Connectivity
The backend connects to Cloud SQL via a Unix socket. Ensure the `NAVALPLAN_DB_SOCKET` secret is set to `/cloudsql/YOUR_PROJECT_ID:REGION:wakelogdb` if not handled automatically by the deployment environment.
