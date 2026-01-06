#!/bin/bash
set -e

# Configuration
REGION="us-central1"
DB_INSTANCE_NAME="wakelogdb"
DB_NAME="navalplan"
DB_USER="navalplan_user"
REPO_NAME="navalplan-repo"

echo "================================================================="
echo "NavalPlan Infrastructure Setup"
echo "================================================================="
echo "This script will set up the core infrastructure for NavalPlan on Google Cloud."
echo "Resources to be created/verified:"
echo " - APIs: Run, SQL Admin, Secret Manager, Build, Artifact Registry, Scheduler"
echo " - Storage: Buckets for logs and system data"
echo " - Database: Cloud SQL (Postgres 15)"
echo " - Registry: Artifact Registry for Docker images"
echo ""

# Get current project
PROJECT_ID=$(gcloud config get-value project 2>/dev/null)
if [ -z "$PROJECT_ID" ]; then
    echo "Error: Could not determine current gcloud project."
    echo "Please run 'gcloud config set project YOUR_PROJECT_ID' first."
    exit 1
fi

echo "Project: $PROJECT_ID"
echo "Region:  $REGION"
echo ""
read -r -p "Press Enter to proceed or Ctrl+C to cancel..."

echo "-----------------------------------------------------------------"
echo "1. Enabling Required APIs..."
gcloud services enable \
    run.googleapis.com \
    sqladmin.googleapis.com \
    secretmanager.googleapis.com \
    compute.googleapis.com \
    storage.googleapis.com \
    artifactregistry.googleapis.com \
    cloudscheduler.googleapis.com \
    cloudbuild.googleapis.com \
    --project="$PROJECT_ID"

echo "APIs enabled."

echo "-----------------------------------------------------------------"
echo "2. Creating Artifact Registry..."
if ! gcloud artifacts repositories describe "$REPO_NAME" --location="$REGION" --project="$PROJECT_ID" >/dev/null 2>&1; then
    gcloud artifacts repositories create "$REPO_NAME" \
        --repository-format=docker \
        --location="$REGION" \
        --description="Docker repository for NavalPlan" \
        --project="$PROJECT_ID"
    echo "Artifact Registry '$REPO_NAME' created."
else
    echo "Artifact Registry '$REPO_NAME' already exists."
fi

echo "-----------------------------------------------------------------"
echo "3. Creating Storage Buckets..."
# Logging bucket
LOG_BUCKET="gs://${PROJECT_ID}-logging" # Using project ID to ensure uniqueness
if ! gsutil ls -b "$LOG_BUCKET" >/dev/null 2>&1; then
    gsutil mb -l "$REGION" "$LOG_BUCKET"
    echo "Created logging bucket: $LOG_BUCKET"
else
    echo "Bucket $LOG_BUCKET already exists."
fi

# System bucket (for SQL imports etc)
SYSTEM_BUCKET="gs://${PROJECT_ID}-system"
if ! gsutil ls -b "$SYSTEM_BUCKET" >/dev/null 2>&1; then
    gsutil mb -l "$REGION" "$SYSTEM_BUCKET"
    echo "Created system bucket: $SYSTEM_BUCKET"
else
    echo "Bucket $SYSTEM_BUCKET already exists."
fi

echo "-----------------------------------------------------------------"
echo "4. Creating Cloud SQL Instance..."
if ! gcloud sql instances describe "$DB_INSTANCE_NAME" --project="$PROJECT_ID" >/dev/null 2>&1; then
    echo "Creating Cloud SQL instance '$DB_INSTANCE_NAME' (this may take a few minutes)..."
    # Basic tier for development/testing to save costs. Adjust tier for production.
    gcloud sql instances create "$DB_INSTANCE_NAME" \
        --database-version=POSTGRES_15 \
        --tier=db-f1-micro \
        --region="$REGION" \
        --project="$PROJECT_ID" \
        --storage-auto-increase
    echo "Instance created."
else
    echo "Cloud SQL instance '$DB_INSTANCE_NAME' already exists."
fi

echo "-----------------------------------------------------------------"
echo "5. Configuring Database..."
# Create Database
if ! gcloud sql databases list --instance="$DB_INSTANCE_NAME" --project="$PROJECT_ID" | grep -q "$DB_NAME"; then
    echo "Creating database '$DB_NAME'..."
    gcloud sql databases create "$DB_NAME" --instance="$DB_INSTANCE_NAME" --project="$PROJECT_ID"
else
    echo "Database '$DB_NAME' already exists."
fi

# Create User
if ! gcloud sql users list --instance="$DB_INSTANCE_NAME" --project="$PROJECT_ID" | grep -q "$DB_USER"; then
    echo "Creating user '$DB_USER'..."
    # Prompt for password
    echo "Enter password for database user '$DB_USER':"
    read -r -s DB_PASS
    gcloud sql users create "$DB_USER" \
        --instance="$DB_INSTANCE_NAME" \
        --password="$DB_PASS" \
        --project="$PROJECT_ID"
    
    # Also update the secret if it exists
    if gcloud secrets describe "NAVALPLAN_DB_PASS" --project="$PROJECT_ID" >/dev/null 2>&1; then
        echo -n "$DB_PASS" | gcloud secrets versions add "NAVALPLAN_DB_PASS" --data-file=-
        echo "Updated NAVALPLAN_DB_PASS secret."
    fi
else
    echo "User '$DB_USER' already exists."
fi

echo "-----------------------------------------------------------------"
echo "Infrastructure setup complete."
echo ""
echo "Next Steps:"
echo "1. Run 'make setup-secrets' to ensure all secrets match your new infrastructure."
echo "2. Update 'cloudbuild.yaml' and 'cloudbuild-agent.yaml' logsBucket to use: $LOG_BUCKET"
echo "3. Update 'Makefile' STORAGE_BUCKET to use: ${PROJECT_ID}-system"
echo "================================================================="
