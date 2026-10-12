#!/bin/bash

# Smoke test for the hydrographic_pilot skill (NOAA Coast Pilot path).
# hydrographic_pilot is a sub-agent tool, so we exercise it through the
# harbourmaster agent, which delegates its pilotage lookup to it.

BASEURL="http://localhost:8081"
APPNAME="harbourmaster"
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
            \"text\": \"Produce a destination briefing for Cape Cod Canal / Buzzards Bay near 41.7400, -70.5700 with a Search Radius of 10 NM. Emphasize the pilot_notes: channel depths, navigation hazards, and bridge clearances from NOAA Coast Pilot. Do not research facilities.\"
        }]
    }
}"

echo "Querying ${APPNAME} (exercises the hydrographic_pilot skill) for NOAA Coast Pilot information..."
curl -X POST \
     -H "Content-Type: application/json" \
     -d "$QUERY" \
     "$ENDPOINT_QUERY" | jq .
