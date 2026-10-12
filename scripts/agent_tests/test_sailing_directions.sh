#!/bin/bash

# Smoke test for the hydrographic_pilot skill (NGA Sailing Directions path).
# hydrographic_pilot is a sub-agent tool, so we exercise it through the
# harbourmaster agent, which delegates its pilotage lookup to it.

BASEURL="http://localhost:8081"
APPNAME="harbourmaster"
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
            \"text\": \"Produce a destination briefing for the Solent / English Channel near 50.7700, -1.3000 with a Search Radius of 10 NM. Emphasize the pilot_notes: navigational hazards, anchorage regulations, and currents from NGA Sailing Directions. Do not research facilities.\"
        }]
    }
}"

echo "Querying ${APPNAME} (exercises the hydrographic_pilot skill) for NGA Sailing Directions information..."
curl -X POST \
     -H "Content-Type: application/json" \
     -d "$QUERY" \
     "$ENDPOINT_QUERY" | jq .
