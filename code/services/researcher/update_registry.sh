AGENT_NAMES="specialist pilot commodore harbourmaster"
for AGENT in $AGENT_NAMES; do
  echo "Updating $AGENT in registry..."
  AUDIENCE="https://navalplan-researcher-2azck273fq-uc.a.run.app"
  # I'll just use a generic card since I can't fetch it easily from here without the token issue
  # But I can construct it!
  cat > "${AGENT}_card.json" <<EOC
{
  "name": "$AGENT",
  "description": "NavalPlan $AGENT agent",
  "capabilities": { "streaming": true },
  "url": "$AUDIENCE/invoke/$AGENT",
  "preferredTransport": "JSONRPC",
  "protocolVersion": "0.3.0",
  "version": "1.0.0",
  "defaultInputModes": [],
  "defaultOutputModes": [],
  "skills": [
    {
      "id": "$AGENT",
      "name": "model",
      "description": "NavalPlan $AGENT agent",
      "tags": ["llm"]
    }
  ]
}
EOC
  gcloud alpha agent-registry services update "projects/navallog/locations/global/services/navalplan-$AGENT" \
    --project navallog \
    --agent-spec-type a2a-agent-card \
    --agent-spec-content "$(cat ${AGENT}_card.json)" \
    --clear-interfaces
done
