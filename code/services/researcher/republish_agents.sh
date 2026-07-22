AGENT_NAMES="lookout specialist pilot commodore harbourmaster"
APP_ID="projects/navallog/locations/global/collections/default_collection/engines/navallog-gemini-enterprise"
BASE_URL="https://navalplan-researcher-2azck273fq-uc.a.run.app/invoke"

for AGENT in $AGENT_NAMES; do
  echo "Republishing $AGENT to Gemini Enterprise..."
  CARD_URL="$BASE_URL/$AGENT/agent-card.json"
  uvx google-agents-cli publish gemini-enterprise \
    --gemini-enterprise-app-id "$APP_ID" \
    --agent-card-url "$CARD_URL" \
    --registration-type a2a \
    --display-name "NavalPlan $AGENT"
done
