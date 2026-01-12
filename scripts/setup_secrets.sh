#!/bin/bash
set -e

# List of secrets to configure
SECRETS=(
    "NAVALPLAN_AGENT_URL"
    "NAVALPLAN_FRONTEND_MAPS_API_KEY"
    "NAVALPLAN_BACKEND_MAPS_API_KEY"
    "NAVALPLAN_MAP_ID"
    "NAVALPLAN_DB_USER"
    "NAVALPLAN_DB_PASS"
    "NAVALPLAN_DB_PORT"
    "NAVALPLAN_DB_MODE"
    "NAVALPLAN_DB_SOCKET"
    "NAVALPLAN_OA_CLIENT"
    "NAVALPLAN_OA_SECRET"
    "NAVALPLAN_SYSTEM_KEY"
)

echo "================================================================="
echo "NavalPlan Cloud Secrets Setup"
echo "================================================================="
echo "This script will help you set up Google Cloud Secret Manager secrets."
echo "Ensure you are authenticated with gcloud and have the correct project selected."
echo ""

# Get current project
PROJECT_ID=$(gcloud config get-value project 2>/dev/null)
if [ -z "$PROJECT_ID" ]; then
    echo "Error: Could not determine current gcloud project."
    echo "Please run 'gcloud config set project YOUR_PROJECT_ID' first."
    exit 1
fi

echo "Current Project: $PROJECT_ID"
echo ""
read -r -p "Press Enter to continue or Ctrl+C to cancel..."

for SECRET_NAME in "${SECRETS[@]}"; do
    echo "-----------------------------------------------------------------"
    echo "Configuring secret: $SECRET_NAME"
    
    # Check if secret exists
    if ! gcloud secrets describe "$SECRET_NAME" --project="$PROJECT_ID" >/dev/null 2>&1; then
        echo "Secret '$SECRET_NAME' does not exist. Creating..."
        gcloud secrets create "$SECRET_NAME" --replication-policy="automatic" --project="$PROJECT_ID"
    else
        echo "Secret '$SECRET_NAME' found."
    fi

    # Prompt for value
    echo "Enter value for $SECRET_NAME (leave empty to skip update):"
    read -r -s SECRET_VALUE
    
    if [ -n "$SECRET_VALUE" ]; then
        # Add new version
        echo -n "$SECRET_VALUE" | gcloud secrets versions add "$SECRET_NAME" --data-file=- --project="$PROJECT_ID"
        echo "Updated $SECRET_NAME."
    else
        echo "Skipping update for $SECRET_NAME."
    fi
    echo ""
done

echo "================================================================="
echo "All secrets processed successfully."
echo "================================================================="
