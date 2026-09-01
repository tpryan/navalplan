#!/bin/bash

BASEURL="http://localhost:8081"
APPNAME="pilot"
USER="testuser"
SESSION="testsession-coast-pilot"

ENDPOINT_SESSION="${BASEURL}/api/apps/${APPNAME}/users/${USER}/sessions/${SESSION}"

echo "Creating session for ${APPNAME}..."
curl -s -X POST "$ENDPOINT_SESSION"
echo -e "\n"

ENDPOINT_QUERY="${BASEURL}/api/run"

QUERY="{
    \"appName\": \"${APPNAME}\",
    \"userId\": \"${USER}\",
    \"sessionId\": \"${SESSION}\",
    \"newMessage\": {
        \"role\": \"user\",
        \"parts\": [{
            \"text\": \"What are the channel depths, navigation hazards, and bridge clearances for Buzzards Bay and Cape Cod Canal according to Coast Pilot?\"
        }]
    }
}"

echo "Querying ${APPNAME} for Coast Pilot information..."
curl -X POST \
     -H "Content-Type: application/json" \
     -d "$QUERY" \
     "$ENDPOINT_QUERY" | jq .
