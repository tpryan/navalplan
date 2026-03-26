#!/bin/bash

BASEURL="http://localhost:8081"
APPNAME="navigator_agent"
USER="testuser"
SESSION="testsession-navigator"

# Endpoint for starting off the adk session
ENDPOINT_SESSION="${BASEURL}/api/apps/${APPNAME}/users/${USER}/sessions/${SESSION}"

echo "Creating session for ${APPNAME}..."
# Make the curl call to start the session
curl -X POST "$ENDPOINT_SESSION"
echo -e "\n"

# Endpoint for querying the NavalPlan agent
ENDPOINT_QUERY="${BASEURL}/api/run"

# Sample agent query, note the session info embedded in it. 
QUERY="{
    \"appName\": \"${APPNAME}\",
    \"userId\": \"${USER}\",
    \"sessionId\": \"${SESSION}\",
    \"newMessage\": {
        \"role\": \"user\",
        \"parts\": [{
        \"text\": \"Analyze the Newport, RI (41.4901, -71.3128) area within a 5 mile radius and identify significant hubs and individual spots for sailing.\"
        }]
    }
}"

echo "Querying ${APPNAME}..."
# Make the curl call
curl -X POST \
     -H "Content-Type: application/json" \
     -d "$QUERY" \
     "$ENDPOINT_QUERY" | jq .
