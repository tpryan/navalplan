#!/bin/bash
set -e

# Tests the POST /api/admin/maintenance endpoint using the system API key,
# mimicking how Cloud Scheduler calls the hourly maintenance task.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env"

if [ -f "$ENV_FILE" ]; then
    export $(grep -v '^#' "$ENV_FILE" | xargs)
fi

BASE_URL="${NAVALPLAN_BASE_URL:-http://localhost:8080}"
SYSTEM_KEY="${NAVALPLAN_SYSTEM_KEY:-}"

if [ -z "$SYSTEM_KEY" ]; then
    echo "Error: NAVALPLAN_SYSTEM_KEY is not set."
    echo "Set it in .env or export it before running this script."
    exit 1
fi

echo "Target: $BASE_URL"
echo "Calling POST /api/admin/maintenance ..."
echo ""

curl -s -w "\nHTTP %{http_code}\n" \
    -X POST \
    -H "Authorization: Bearer $SYSTEM_KEY" \
    -H "Content-Type: application/json" \
    "$BASE_URL/api/admin/maintenance"

echo ""
