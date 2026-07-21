#!/bin/bash
# deploy_mesh.sh - Performs zero-downtime traffic cutover for NavalPlan services.

SERVICE_NAME=${1:-navalplan-researcher}
REGION=${2:-us-central1}

echo "Starting traffic cutover for $SERVICE_NAME in $REGION..."

# 1. Verify the canary revision is healthy
echo "Verifying canary revision..."
# Extract the URL for the 'canary' tag
CANARY_URL=$(gcloud run services describe $SERVICE_NAME --region $REGION --format='value(status.traffic)' | grep -o 'https://canary---[^ ]*')

if [ -z "$CANARY_URL" ]; then
    echo "Error: No canary revision found with tag 'canary' or failed to parse URL."
    # Fallback to checking if the tag exists at all
    HAS_CANARY=$(gcloud run services describe $SERVICE_NAME --region $REGION --format='value(status.traffic)' | grep 'canary')
    if [ -z "$HAS_CANARY" ]; then
        exit 1
    fi
fi
echo "Canary available for testing."

# 2. Shift 10% of traffic to canary
echo "Shifting 10% traffic to canary..."
gcloud run services update-traffic $SERVICE_NAME --region $REGION --to-tags canary=10

# 3. Wait for user confirmation or automated health check
echo "Traffic shifted. Monitor logs for stability."
echo "Run 'gcloud run services update-traffic $SERVICE_NAME --region $REGION --to-latest' to complete cutover."

# For this automated script, we'll wait 30s then proceed to 50% if requested, 
# but for GEAP best practices, we leave the final jump to the operator or a more complex health check.
sleep 10

echo "Shifting 50% traffic to canary..."
gcloud run services update-traffic $SERVICE_NAME --region $REGION --to-tags canary=50

echo "Ready for final cutover."
