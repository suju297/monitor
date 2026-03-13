from __future__ import annotations

import logging
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import Any

from .adapters import fetch_jobs_for_source
from .exceptions import CrawlBlockedError
from .http_client import requests_session
from .models import CrawlOutcome
from .policy import base_source_plan, resolve_source_plan
from .utils import is_us_based_job, parse_bool_env


def orchestrate_company(
    company: dict[str, Any],
    prior_status: dict[str, Any] | None,
) -> CrawlOutcome:
    plan = resolve_source_plan(company, prior_status=prior_status)
    us_only_jobs = parse_bool_env("US_ONLY_JOBS", True)
    attempted: list[str] = []
    session = requests_session()
    last_error: Exception | None = None
    last_blocked: Exception | None = None

    for source in plan:
        attempted.append(source)
        try:
            jobs = fetch_jobs_for_source(company, source, session)
            if us_only_jobs:
                jobs = [
                    job
                    for job in jobs
                    if is_us_based_job(
                        title=job.title,
                        location=job.location,
                        url=job.url,
                    )
                ]
            return CrawlOutcome(
                company=company["name"],
                attempted_sources=attempted,
                status="ok",
                selected_source=source,
                jobs=jobs,
                message=f"{len(jobs)} job(s) fetched",
            )
        except CrawlBlockedError as exc:
            last_blocked = exc
            logging.warning("[%s] source '%s' blocked: %s", company["name"], source, exc)
            continue
        except Exception as exc:  # noqa: BLE001
            last_error = exc
            logging.warning("[%s] source '%s' failed: %s", company["name"], source, exc)
            continue

    if last_blocked is not None and last_error is None:
        return CrawlOutcome(
            company=company["name"],
            attempted_sources=attempted,
            status="blocked",
            message=str(last_blocked),
        )
    if last_blocked is not None and last_error is not None:
        return CrawlOutcome(
            company=company["name"],
            attempted_sources=attempted,
            status="blocked",
            message=f"{last_blocked} | secondary error: {last_error}",
        )
    return CrawlOutcome(
        company=company["name"],
        attempted_sources=attempted,
        status="error",
        message=str(last_error or "unknown error"),
    )


def crawl_companies(
    companies: list[dict[str, Any]],
    workers: int,
    prior_company_status: dict[str, dict[str, Any]],
) -> list[CrawlOutcome]:
    max_workers = max(1, min(workers, len(companies)))
    outcomes: list[CrawlOutcome] = []
    with ThreadPoolExecutor(max_workers=max_workers) as pool:
        future_to_company = {
            pool.submit(
                orchestrate_company,
                company,
                prior_company_status.get(company["name"]),
            ): company
            for company in companies
        }
        for future in as_completed(future_to_company):
            company = future_to_company[future]
            try:
                outcome = future.result()
            except Exception as exc:  # noqa: BLE001
                outcome = CrawlOutcome(
                    company=company["name"],
                    attempted_sources=base_source_plan(company),
                    status="error",
                    message=f"Unhandled worker exception: {exc}",
                )
            outcomes.append(outcome)
    outcomes.sort(key=lambda item: item.company.lower())
    return outcomes
