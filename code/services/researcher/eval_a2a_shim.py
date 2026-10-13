"""Eval-only shim for evaluating the Go researcher agents over A2A.

Why this exists
---------------
Newer ``google-adk`` (>= ~1.28) collapses a non-streaming A2A *Task* response
into a single ADK ``Event`` that carries the function calls, the function
responses, AND the agent's final text all at once. Because that event contains
function parts, ``Event.is_final_response()`` returns ``False``, so the
evaluation harness (``evaluation_generator``) never records an
``Invocation.final_response``. That silently breaks any
``rubric_based_final_response_quality_v1`` test (e.g. ``lookout`` and
``specialist``) even though the agent produced correct output -- the judge has
nothing to score.

Older ``google-adk`` captured ``final_response`` directly, which is why the same
agents passed before. Rather than pin ``google-adk`` to an old version (we want
newer features), this shim fixes the symptom in a version-independent way.

What it does
------------
``NormalizedRemoteA2aAgent`` wraps ``RemoteA2aAgent``. It forwards every event
unchanged (so the tool-use trace is preserved for ``tool_use_quality``), and
when the final text was bundled into an event alongside function parts, it
re-emits that text as a standalone, function-free event. That event satisfies
``is_final_response()``, so the harness captures ``final_response`` again.
If the remote agent already emits a clean final-text event, nothing extra is
emitted (no duplicate).
"""

from __future__ import annotations

import os
from typing import AsyncGenerator, Optional

from google.adk.agents.invocation_context import InvocationContext
from google.adk.agents.remote_a2a_agent import RemoteA2aAgent
from google.adk.events.event import Event
from google.genai import types as genai_types


class NormalizedRemoteA2aAgent(RemoteA2aAgent):
    """RemoteA2aAgent that guarantees a capturable final-response event."""

    async def _run_async_impl(
        self, ctx: InvocationContext
    ) -> AsyncGenerator[Event, None]:
        final_text_parts: Optional[list] = None
        async for event in super()._run_async_impl(ctx):
            yield event
            if not (event.content and event.content.parts):
                continue
            if event.is_final_response():
                # The harness already captures this cleanly; don't duplicate.
                final_text_parts = None
                continue
            text_parts = [
                p
                for p in event.content.parts
                if getattr(p, "text", None) and not getattr(p, "thought", False)
            ]
            if text_parts and (
                event.get_function_calls() or event.get_function_responses()
            ):
                # Final text was bundled with function parts -> remember it so
                # we can re-emit it as a standalone final event below.
                final_text_parts = text_parts
        if final_text_parts:
            yield Event(
                author=self.name,
                invocation_id=ctx.invocation_id,
                branch=ctx.branch,
                content=genai_types.Content(role="model", parts=final_text_parts),
            )


# A2A HTTP timeout for eval, in seconds. RemoteA2aAgent defaults to 600s; slower
# models (e.g. larger Gemini variants) on tool-heavy agents like harbourmaster
# can exceed that and otherwise return an EMPTY response that fails every rubric.
# Override with the NAVALPLAN_EVAL_A2A_TIMEOUT env var.
_DEFAULT_EVAL_A2A_TIMEOUT = 1800.0


def _eval_a2a_timeout(explicit: Optional[float]) -> float:
    if explicit is not None:
        return explicit
    env = os.environ.get("NAVALPLAN_EVAL_A2A_TIMEOUT", "").strip()
    if env:
        try:
            return float(env)
        except ValueError:
            pass
    return _DEFAULT_EVAL_A2A_TIMEOUT


def make_root_agent(
    name: str,
    agent_card: str,
    use_legacy: bool = False,
    timeout: Optional[float] = None,
) -> NormalizedRemoteA2aAgent:
    """Build the eval root agent that talks to the Go service's A2A endpoint.

    The A2A HTTP timeout defaults to NAVALPLAN_EVAL_A2A_TIMEOUT (or 1800s) rather
    than RemoteA2aAgent's 600s, so a slow model isn't scored on an empty,
    timed-out response.
    """
    return NormalizedRemoteA2aAgent(
        name=name,
        agent_card=agent_card,
        use_legacy=use_legacy,
        timeout=_eval_a2a_timeout(timeout),
    )


# ---------------------------------------------------------------------------
# Eval harness compatibility patch (import-time, eval process only).
#
# Newer google-adk's `rubric_based_final_response_quality_v1` builds the judge
# prompt by calling
#     app_details.get_developer_instructions(agent_name=<first event author>)
# and RAISES `ValueError("<name> not found in the agentic system.")` when that
# agent is absent from `app_details.agent_details`.
#
# When evaluating a remote agent over A2A, `agent_details` is ALWAYS empty: the
# eval process never observes the remote agent's model requests, so it has no
# instructions to record. For any agent that makes tool calls AND whose final
# response is captured (e.g. lookout), this call therefore crashes the judge and
# every case is marked NOT_EVALUATED -- even though the agent produced correct
# output. (Agents whose trace has no intermediate events never reach the call.)
#
# We degrade gracefully instead: return "" (no developer instructions) when the
# agent is unknown, so the judge still scores the final response against the
# rubrics. This changes nothing when agent_details IS populated.
# ---------------------------------------------------------------------------
try:
    from google.adk.evaluation.app_details import AppDetails as _AppDetails

    def _safe_get_developer_instructions(self, agent_name: str) -> str:
        details = self.agent_details.get(agent_name)
        return details.instructions if details is not None else ""

    _AppDetails.get_developer_instructions = _safe_get_developer_instructions
except Exception:  # pragma: no cover - never let a patch failure break eval
    pass
