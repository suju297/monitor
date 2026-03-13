from __future__ import annotations

import hashlib
from typing import Any

from .models import CrawlOutcome, Job
from .utils import utc_now


def job_fingerprint(job: Job) -> str:
    base = "|".join(
        [
            job.company.strip().lower(),
            (job.external_id or "").strip().lower(),
            job.url.strip().lower(),
            job.title.strip().lower(),
        ]
    )
    return hashlib.sha256(base.encode("utf-8")).hexdigest()


def grouped_jobs(jobs: list[Job]) -> dict[str, list[Job]]:
    grouped: dict[str, list[Job]] = {}
    for job in jobs:
        grouped.setdefault(job.company, []).append(job)
    for company_jobs in grouped.values():
        company_jobs.sort(key=lambda item: item.title.lower())
    return dict(sorted(grouped.items(), key=lambda item: item[0].lower()))


def apply_outcomes_to_state(
    outcomes: list[CrawlOutcome],
    state: dict[str, Any],
) -> tuple[list[Job], list[CrawlOutcome], list[str]]:
    seen_by_company = state.setdefault("seen", {})
    company_status = state.setdefault("company_status", {})
    blocked_events = state.setdefault("blocked_events", {})

    new_jobs: list[Job] = []
    blocked_outcomes: list[CrawlOutcome] = []
    status_lines: list[str] = []

    for outcome in outcomes:
        company_status[outcome.company] = {
            "status": outcome.status,
            "selected_source": outcome.selected_source,
            "attempted_sources": outcome.attempted_sources,
            "message": outcome.message,
            "updated_at": utc_now(),
        }

        if outcome.status == "ok":
            company_seen: dict[str, dict[str, str]] = seen_by_company.setdefault(outcome.company, {})
            company_new_count = 0
            for job in outcome.jobs:
                fingerprint = job_fingerprint(job)
                if fingerprint in company_seen:
                    continue
                company_seen[fingerprint] = {
                    "title": job.title,
                    "url": job.url,
                    "first_seen": utc_now(),
                }
                new_jobs.append(job)
                company_new_count += 1
            status_lines.append(
                f"[OK] {outcome.company} ({outcome.selected_source}): "
                f"{len(outcome.jobs)} found, {company_new_count} new"
            )
            continue

        if outcome.status == "blocked":
            blocked_outcomes.append(outcome)
            history = blocked_events.setdefault(outcome.company, [])
            history.append(
                {
                    "at": utc_now(),
                    "attempted_sources": outcome.attempted_sources,
                    "message": outcome.message,
                }
            )
            blocked_events[outcome.company] = history[-30:]
            status_lines.append(
                f"[BLOCKED] {outcome.company} ({' -> '.join(outcome.attempted_sources)}): {outcome.message}"
            )
            continue

        status_lines.append(
            f"[ERROR] {outcome.company} ({' -> '.join(outcome.attempted_sources)}): {outcome.message}"
        )

    return new_jobs, blocked_outcomes, status_lines

