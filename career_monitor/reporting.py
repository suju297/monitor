from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from .models import CrawlOutcome, Job
from .utils import utc_now


def _job_payload(job: Job) -> dict[str, Any]:
    return {
        "company": job.company,
        "source": job.source,
        "title": job.title,
        "url": job.url,
        "external_id": job.external_id,
        "location": job.location,
        "team": job.team,
        "posted_at": job.posted_at,
    }


def _outcome_payload(outcome: CrawlOutcome) -> dict[str, Any]:
    return {
        "company": outcome.company,
        "status": outcome.status,
        "selected_source": outcome.selected_source,
        "attempted_sources": outcome.attempted_sources,
        "message": outcome.message,
        "jobs_count": len(outcome.jobs),
    }


def write_run_report(
    report_path: Path,
    outcomes: list[CrawlOutcome],
    new_jobs: list[Job],
    blocked_count: int,
    dry_run: bool,
    baseline: bool,
) -> None:
    report = {
        "generated_at": utc_now(),
        "dry_run": dry_run,
        "baseline": baseline,
        "summary": {
            "companies_total": len(outcomes),
            "new_jobs_count": len(new_jobs),
            "blocked_count": blocked_count,
            "ok_count": len([item for item in outcomes if item.status == "ok"]),
            "error_count": len([item for item in outcomes if item.status == "error"]),
        },
        "outcomes": [_outcome_payload(outcome) for outcome in outcomes],
        "new_jobs": [_job_payload(job) for job in new_jobs],
    }
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(json.dumps(report, indent=2, sort_keys=True), encoding="utf-8")

