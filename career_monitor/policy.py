from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from .constants import SUPPORTED_SOURCES


def unique_plan(raw_sources: list[str]) -> list[str]:
    deduped: list[str] = []
    for source in raw_sources:
        if source not in deduped:
            deduped.append(source)
    return deduped


def base_source_plan(company: dict[str, Any]) -> list[str]:
    return unique_plan([company["source"], *company.get("fallback_sources", [])])


def adaptive_source_plan(company: dict[str, Any], prior_status: dict[str, Any] | None) -> list[str]:
    plan = base_source_plan(company)
    if not prior_status:
        return plan
    if str(prior_status.get("status", "")).lower() != "blocked":
        return plan
    attempted = prior_status.get("attempted_sources")
    if not isinstance(attempted, list) or not attempted:
        return plan

    blocked_primary = str(attempted[0]).strip().lower()
    if blocked_primary not in plan:
        return plan

    # Move last blocked primary source to the end for this run.
    reordered = [source for source in plan if source != blocked_primary]
    reordered.append(blocked_primary)
    return unique_plan(reordered)


def _policy_hook_plan(payload: dict[str, Any]) -> list[str] | None:
    policy_cmd = os.getenv("ORCHESTRATOR_POLICY_CMD", "").strip()
    if not policy_cmd:
        return None

    process = subprocess.run(  # noqa: S603,S607
        policy_cmd,
        shell=True,
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        check=False,
    )
    if process.returncode != 0:
        return None
    try:
        response = json.loads(process.stdout or "{}")
    except json.JSONDecodeError:
        return None
    plan = response.get("plan")
    if not isinstance(plan, list):
        return None
    normalized: list[str] = []
    for source in plan:
        value = str(source).strip().lower()
        if value in SUPPORTED_SOURCES and value not in normalized:
            normalized.append(value)
    if not normalized:
        return None
    return normalized


def resolve_source_plan(company: dict[str, Any], prior_status: dict[str, Any] | None) -> list[str]:
    payload = {"company": company, "prior_status": prior_status}
    hook = _policy_hook_plan(payload)
    if hook:
        allowed = set(base_source_plan(company))
        constrained = [source for source in hook if source in allowed]
        if constrained:
            return constrained
    return adaptive_source_plan(company, prior_status)
