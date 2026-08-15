"""Nautical domain custom evaluation functions for Agent Platform evaluation."""

import json
import re


def validate_json(instance):
    """Evaluates whether the agent response contains valid JSON (object or array)."""
    resp_text = ""
    response = instance.get("response") or {}
    if isinstance(response, dict):
        parts = response.get("parts", [])
        for p in parts:
            if isinstance(p, dict) and "text" in p:
                resp_text += p["text"]
    elif isinstance(response, str):
        resp_text = response

    text = resp_text.strip()
    if text.startswith("```json"):
        text = text[7:]
    elif text.startswith("```"):
        text = text[3:]
    if text.endswith("```"):
        text = text[:-3]
    text = text.strip()

    if not text:
        return {"score": 0.0, "explanation": "Response text is empty."}

    try:
        parsed = json.loads(text)
        if isinstance(parsed, (dict, list)):
            return {"score": 1.0, "explanation": "Successfully parsed valid JSON."}
        return {"score": 0.5, "explanation": "Parsed JSON is not an object or array."}
    except Exception as e:
        return {"score": 0.0, "explanation": f"Failed to parse JSON: {e}"}


def validate_safety_alerts(instance):
    """Evaluates whether the Lookout agent returns well-formed safety alerts."""
    resp_text = ""
    response = instance.get("response") or {}
    if isinstance(response, dict):
        parts = response.get("parts", [])
        for p in parts:
            if isinstance(p, dict) and "text" in p:
                resp_text += p["text"]
    elif isinstance(response, str):
        resp_text = response

    text = resp_text.strip()
    if text.startswith("```json"):
        text = text[7:]
    elif text.startswith("```"):
        text = text[3:]
    if text.endswith("```"):
        text = text[:-3]
    text = text.strip()

    try:
        alerts = json.loads(text)
        if not isinstance(alerts, list):
            return {"score": 0.0, "explanation": "Expected a JSON array of alerts."}

        valid_severities = {"danger", "warning", "info"}
        valid_categories = {"weather", "tides", "navigation", "sun"}

        for alert in alerts:
            if not isinstance(alert, dict):
                return {"score": 0.0, "explanation": "Alert item is not a dictionary."}
            if alert.get("severity") not in valid_severities:
                return {
                    "score": 0.5,
                    "explanation": f"Invalid severity: {alert.get('severity')}",
                }
            if alert.get("category") not in valid_categories:
                return {
                    "score": 0.5,
                    "explanation": f"Invalid category: {alert.get('category')}",
                }
            if not alert.get("message") or not isinstance(alert.get("message"), str):
                return {"score": 0.5, "explanation": "Alert missing message string."}

        return {"score": 1.0, "explanation": "All alerts passed schema validation."}
    except Exception as e:
        return {"score": 0.0, "explanation": f"Alert JSON parsing error: {e}"}
