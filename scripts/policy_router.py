#!/usr/bin/env python3
from __future__ import annotations

import json
import sys


def unique_plan(values: list[str]) -> list[str]:
    deduped: list[str] = []
    for value in values:
        if value and value not in deduped:
            deduped.append(value)
    return deduped


def profile_tuning(plan: list[str], profile: str) -> list[str]:
    if profile == "browser-first":
        priority = [source for source in ["playwright", "command"] if source in plan]
        return unique_plan(priority + plan)
    if profile == "api-first":
        priority = [
            source
            for source in ["greenhouse", "lever", "icims", "icms", "template"]
            if source in plan
        ]
        return unique_plan(priority + plan)
    return plan


def rotate_last_blocked(plan: list[str], prior_status: dict) -> list[str]:
    if str(prior_status.get("status", "")).lower() != "blocked":
        return plan
    attempted = prior_status.get("attempted_sources")
    if not isinstance(attempted, list) or not attempted:
        return plan
    blocked_primary = str(attempted[0]).strip().lower()
    if blocked_primary not in plan:
        return plan
    rotated = [source for source in plan if source != blocked_primary]
    rotated.append(blocked_primary)
    return rotated


def main() -> int:
    raw = sys.stdin.read().strip() or "{}"
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        print(json.dumps({"plan": []}))
        return 0

    company = payload.get("company") if isinstance(payload.get("company"), dict) else {}
    prior_status = (
        payload.get("prior_status") if isinstance(payload.get("prior_status"), dict) else {}
    )
    base = [str(company.get("source", "")).strip().lower()]
    fallback = company.get("fallback_sources")
    if isinstance(fallback, list):
        base.extend(str(item).strip().lower() for item in fallback)
    plan = unique_plan(base)

    profile = ""
    orchestration = company.get("orchestration")
    if isinstance(orchestration, dict):
        profile = str(orchestration.get("profile", "")).strip().lower()

    plan = profile_tuning(plan, profile)
    plan = rotate_last_blocked(plan, prior_status)
    print(json.dumps({"plan": plan}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
