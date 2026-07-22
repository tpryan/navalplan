#!/bin/bash
set -e

# This script registers the NavalPlan MCP service with the Agent Registry.

SERVICE_NAME="navalplan-mcp"
REGION="global" # Agent Registry services are usually global or regional
PROJECT_ID=$(gcloud config get-value project)

# Get the URL of the deployed researcher service
RESEARCHER_URL=$(gcloud run services describe navalplan-researcher --region us-central1 --format='value(status.url)')
MCP_URL="${RESEARCHER_URL}/mcp/tools"

echo "Registering $SERVICE_NAME with Agent Registry..."
echo "URL: $MCP_URL"

# Try to create, fallback to update
gcloud alpha agent-registry services create $SERVICE_NAME \
    --project=$PROJECT_ID \
    --location=$REGION \
    --display-name="NavalPlan MCP Tools" \
    --description="Provides nautical tools for tides, weather, and safety alerts." \
    --mcp-server-spec-type=no-spec \
    --interfaces="url=$MCP_URL,protocolBinding=mcp" || \
gcloud alpha agent-registry services update $SERVICE_NAME \
    --project=$PROJECT_ID \
    --location=$REGION \
    --display-name="NavalPlan MCP Tools" \
    --description="Provides nautical tools for tides, weather, and safety alerts." \
    --mcp-server-spec-type=no-spec \
    --interfaces="url=$MCP_URL,protocolBinding=mcp"

echo "Registration complete."
