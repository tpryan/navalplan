#!/bin/bash

BASEURL="http://localhost:8081"
APPNAME="pilot"
USER="testuser"
SESSION="testsession-sailing-directions"

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
            \"text\": \"What are the navigational hazards, anchorage regulations, and currents when approaching the English Channel and Solent according to NGA Sailing Directions?\"
        }]
    }
}"

echo "Querying ${APPNAME} for NGA Sailing Directions information..."
curl -X POST \
     -H "Content-Type: application/json" \
     -d "$QUERY" \
     "$ENDPOINT_QUERY" | jq .
