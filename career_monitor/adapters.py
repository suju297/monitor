from __future__ import annotations

import json
import os
import subprocess
from typing import Any
from urllib.parse import urljoin, urlparse

import requests
from bs4 import BeautifulSoup

from .exceptions import CrawlBlockedError, CrawlFetchError
from .http_client import perform_request
from .models import Job
from .utils import (
    build_icims_job_url,
    get_nested,
    looks_like_job_link,
    normalize_created_at,
    parse_greenhouse_board,
    parse_icims_api_endpoint,
    parse_lever_site,
    title_from_url,
)

MY_GREENHOUSE_DEFAULT_COMMAND = ["uv", "run", "python", "scripts/fetch_my_greenhouse_jobs.py"]


def _run_job_command(
    company: dict[str, Any],
    command: str | list[str],
    env_updates: dict[str, str] | None = None,
) -> list[Job]:
    if isinstance(command, str):
        shell = True
        command_value: str | list[str] = command
    elif isinstance(command, list):
        shell = False
        command_value = [str(token) for token in command]
    else:
        raise CrawlFetchError(f"Company '{company['name']}' has invalid command type.")

    env = os.environ.copy()
    env["COMPANY_NAME"] = company["name"]
    env["CAREERS_URL"] = company["careers_url"]
    env.update({str(key): str(value) for key, value in company.get("command_env", {}).items()})
    if env_updates:
        env.update({str(key): str(value) for key, value in env_updates.items()})

    process = subprocess.run(  # noqa: S603
        command_value,  # noqa: S607
        shell=shell,
        capture_output=True,
        text=True,
        timeout=company["timeout_seconds"],
        env=env,
        check=False,
    )
    stdout = process.stdout.strip()
    stderr = process.stderr.strip()
    if process.returncode != 0:
        combined = stderr or stdout or "Command failed without output."
        raise CrawlFetchError(
            f"Command adapter failed for {company['name']} (exit {process.returncode}): {combined}"
        )
    try:
        payload = json.loads(stdout or "[]")
    except json.JSONDecodeError as exc:
        raise CrawlFetchError(
            f"Command adapter for {company['name']} did not output valid JSON."
        ) from exc

    if isinstance(payload, dict):
        status = str(payload.get("status", "ok")).strip().lower()
        message = str(payload.get("message", "")).strip()
        jobs_payload = payload.get("jobs", [])
        if status == "blocked":
            raise CrawlBlockedError(message or "Command adapter reported blocked target.")
        if status == "error":
            raise CrawlFetchError(message or "Command adapter reported error.")
        if status != "ok":
            raise CrawlFetchError(
                f"Command adapter for {company['name']} returned unknown status '{status}'."
            )
        payload = jobs_payload

    if not isinstance(payload, list):
        raise CrawlFetchError(
            f"Command adapter output for {company['name']} must be a JSON list or object."
        )

    jobs: list[Job] = []
    for item in payload:
        if not isinstance(item, dict):
            continue
        title = str(item.get("title", "")).strip()
        url_value = str(item.get("url", "")).strip()
        if not title or not url_value:
            continue
        jobs.append(
            Job(
                company=company["name"],
                source="command",
                title=title,
                url=url_value,
                external_id=(str(item.get("external_id", "")).strip() or None),
                location=(str(item.get("location", "")).strip() or None),
                team=(str(item.get("team", "")).strip() or None),
                posted_at=(str(item.get("posted_at", "")).strip() or None),
            )
        )
    return jobs


def fetch_greenhouse(company: dict[str, Any], session: requests.Session) -> list[Job]:
    board = (company.get("greenhouse_board") or "").strip() or parse_greenhouse_board(
        company["careers_url"]
    )
    if not board:
        raise CrawlFetchError(
            f"Could not derive greenhouse board from '{company['careers_url']}'."
        )
    endpoint = f"https://boards-api.greenhouse.io/v1/boards/{board}/jobs?content=true"
    response = perform_request(
        session=session,
        method="GET",
        url=endpoint,
        timeout_seconds=company["timeout_seconds"],
    )
    payload = response.json()
    jobs: list[Job] = []
    for item in payload.get("jobs", []):
        title = (item.get("title") or "").strip()
        url = (item.get("absolute_url") or item.get("url") or "").strip()
        if not title or not url:
            continue
        departments = item.get("departments") or []
        team = departments[0].get("name") if departments else None
        location = (item.get("location") or {}).get("name")
        jobs.append(
            Job(
                company=company["name"],
                source="greenhouse",
                title=title,
                url=url,
                external_id=str(item.get("id", "")) or None,
                location=location,
                team=team,
                posted_at=item.get("updated_at"),
            )
        )
    return jobs


def fetch_lever(company: dict[str, Any], session: requests.Session) -> list[Job]:
    site = (company.get("lever_site") or "").strip() or parse_lever_site(company["careers_url"])
    if not site:
        raise CrawlFetchError(f"Could not derive lever site from '{company['careers_url']}'.")
    endpoint = f"https://api.lever.co/v0/postings/{site}?mode=json"
    response = perform_request(
        session=session,
        method="GET",
        url=endpoint,
        timeout_seconds=company["timeout_seconds"],
    )
    payload = response.json()
    jobs: list[Job] = []
    for item in payload:
        title = (item.get("text") or "").strip()
        url = (item.get("hostedUrl") or item.get("applyUrl") or "").strip()
        if not title or not url:
            continue
        categories = item.get("categories") or {}
        jobs.append(
            Job(
                company=company["name"],
                source="lever",
                title=title,
                url=url,
                external_id=item.get("id"),
                location=categories.get("location"),
                team=categories.get("team"),
                posted_at=normalize_created_at(item.get("createdAt")),
            )
        )
    return jobs


def _int_command_env(company: dict[str, Any], key: str, default: int, minimum: int = 1) -> int:
    raw = str((company.get("command_env", {}) or {}).get(key, "")).strip()
    if not raw:
        return default
    try:
        parsed = int(raw)
    except ValueError:
        return default
    return parsed if parsed >= minimum else default


def _bool_command_env(company: dict[str, Any], key: str, default: bool) -> bool:
    raw = str((company.get("command_env", {}) or {}).get(key, "")).strip().lower()
    if not raw:
        return default
    if raw in {"1", "true", "yes", "on"}:
        return True
    if raw in {"0", "false", "no", "off"}:
        return False
    return default


def _icims_location(payload: dict[str, Any]) -> str | None:
    primary = (payload.get("full_location") or payload.get("location_name") or payload.get("short_location") or "").strip()
    if primary:
        return primary
    parts = [
        str(payload.get("city") or "").strip(),
        str(payload.get("state") or "").strip(),
        str(payload.get("country") or "").strip(),
    ]
    compact = ", ".join(part for part in parts if part)
    return compact or None


def _icims_team(payload: dict[str, Any]) -> str | None:
    labels: list[str] = []
    categories = payload.get("categories")
    if isinstance(categories, list):
        for item in categories:
            if not isinstance(item, dict):
                continue
            name = str(item.get("name", "")).strip()
            if name and name not in labels:
                labels.append(name)
    raw_category = payload.get("category")
    if isinstance(raw_category, list):
        for value in raw_category:
            label = str(value).strip()
            if label and label not in labels:
                labels.append(label)
    return " | ".join(labels) or None


def fetch_icims(company: dict[str, Any], session: requests.Session) -> list[Job]:
    endpoint = parse_icims_api_endpoint(company["careers_url"])
    if not endpoint:
        raise CrawlFetchError(
            f"Could not derive iCIMS API endpoint from '{company['careers_url']}'."
        )

    max_pages = _int_command_env(company, "ICIMS_MAX_PAGES", 25)
    max_jobs = _int_command_env(company, "ICIMS_MAX_JOBS", 800)
    sort_by = str((company.get("command_env", {}) or {}).get("ICIMS_SORT_BY", "relevance")).strip() or "relevance"
    descending = _bool_command_env(company, "ICIMS_DESCENDING", False)
    internal = _bool_command_env(company, "ICIMS_INTERNAL", False)

    jobs: list[Job] = []
    seen: set[str] = set()
    total_pages = max_pages

    for page in range(1, max_pages + 1):
        response = perform_request(
            session=session,
            method="GET",
            url=endpoint,
            timeout_seconds=company["timeout_seconds"],
            params={
                "page": page,
                "sortBy": sort_by,
                "descending": str(descending).lower(),
                "internal": str(internal).lower(),
            },
        )
        payload = response.json()
        if isinstance(payload.get("count"), int) and payload["count"] > 0:
            total_pages = min(max_pages, int(payload["count"]))
        rows = payload.get("jobs")
        if not isinstance(rows, list):
            raise CrawlFetchError(f"iCIMS payload did not include a jobs list for {company['name']}.")
        if not rows:
            break

        for row in rows:
            if not isinstance(row, dict):
                continue
            data = row.get("data")
            if not isinstance(data, dict):
                continue
            title = str(data.get("title", "")).strip()
            req_id = str(data.get("req_id", "")).strip()
            canonical = str(get_nested(data, "meta_data.canonical_url", "")).strip() or None
            language = str(data.get("language", "")).strip() or None
            job_url = build_icims_job_url(company["careers_url"], req_id=req_id, language=language, canonical_url=canonical)
            if not title or not job_url:
                continue

            dedupe_key = req_id.lower() or job_url.lower()
            if dedupe_key in seen:
                continue
            seen.add(dedupe_key)

            posted_at = (
                normalize_created_at(data.get("posted_date"))
                or normalize_created_at(data.get("create_date"))
                or normalize_created_at(data.get("update_date"))
            )
            jobs.append(
                Job(
                    company=company["name"],
                    source="icims",
                    title=title,
                    url=job_url,
                    external_id=req_id or None,
                    location=_icims_location(data),
                    team=_icims_team(data),
                    posted_at=posted_at,
                )
            )
            if len(jobs) >= max_jobs:
                return jobs
        if page >= total_pages:
            break

    return jobs


def fetch_generic(company: dict[str, Any], session: requests.Session) -> list[Job]:
    response = perform_request(
        session=session,
        method="GET",
        url=company["careers_url"],
        timeout_seconds=company["timeout_seconds"],
    )
    soup = BeautifulSoup(response.text, "html.parser")
    jobs: list[Job] = []
    seen_urls: set[str] = set()
    max_links = max(1, int(company.get("max_links", 200)))
    for anchor in soup.select("a[href]"):
        href = (anchor.get("href") or "").strip()
        if not href or href.startswith(("#", "mailto:", "tel:", "javascript:")):
            continue
        absolute_url = urljoin(company["careers_url"], href)
        parsed = urlparse(absolute_url)
        if parsed.scheme not in {"http", "https"}:
            continue
        text = " ".join(anchor.get_text(" ", strip=True).split())
        if not looks_like_job_link(text, absolute_url):
            continue
        if absolute_url in seen_urls:
            continue
        seen_urls.add(absolute_url)
        jobs.append(
            Job(
                company=company["name"],
                source="generic",
                title=(text or title_from_url(absolute_url)),
                url=absolute_url,
            )
        )
        if len(jobs) >= max_links:
            break
    return jobs


def _read_template_field(item: dict[str, Any], field_name: str, fallback: str = "") -> str:
    value = get_nested(item, field_name, default=fallback)
    if value is None:
        return ""
    return str(value).strip()


def fetch_template(company: dict[str, Any], session: requests.Session) -> list[Job]:
    template = company.get("template")
    if not isinstance(template, dict) or not template:
        raise CrawlFetchError(f"Company '{company['name']}' source template requires template config.")

    method = str(template.get("method", "GET")).upper()
    url = str(template.get("url", "")).strip() or company["careers_url"]
    response = perform_request(
        session=session,
        method=method,
        url=url,
        timeout_seconds=company["timeout_seconds"],
        params=template.get("params"),
        headers=template.get("headers"),
        json=template.get("json"),
        data=template.get("data"),
    )
    try:
        payload = response.json()
    except ValueError as exc:
        raise CrawlFetchError(f"Template endpoint did not return valid JSON: {url}") from exc

    jobs_path = str(template.get("jobs_path", "")).strip()
    fields = template.get("fields") if isinstance(template.get("fields"), dict) else {}
    list_payload = get_nested(payload, jobs_path) if jobs_path else payload
    if isinstance(list_payload, dict):
        list_payload = list_payload.get("jobs", [])
    if not isinstance(list_payload, list):
        raise CrawlFetchError(
            f"Template jobs_path '{jobs_path or '(root)'}' did not resolve to a list for {company['name']}."
        )

    base_url = str(template.get("base_url", "")).strip() or company["careers_url"]
    id_field = str(fields.get("id", "id"))
    title_field = str(fields.get("title", "title"))
    url_field = str(fields.get("url", "url"))
    location_field = str(fields.get("location", "location"))
    team_field = str(fields.get("team", "team"))
    posted_at_field = str(fields.get("posted_at", "posted_at"))

    jobs: list[Job] = []
    for item in list_payload:
        if not isinstance(item, dict):
            continue
        title = _read_template_field(item, title_field)
        url_value = _read_template_field(item, url_field)
        if not title or not url_value:
            continue
        jobs.append(
            Job(
                company=company["name"],
                source="template",
                title=title,
                url=urljoin(base_url, url_value),
                external_id=_read_template_field(item, id_field) or None,
                location=_read_template_field(item, location_field) or None,
                team=_read_template_field(item, team_field) or None,
                posted_at=_read_template_field(item, posted_at_field) or None,
            )
        )
    return jobs


def fetch_command(company: dict[str, Any]) -> list[Job]:
    command = company.get("command")
    if not command:
        raise CrawlFetchError(
            f"Company '{company['name']}' source command/playwright requires 'command' in config."
        )
    return _run_job_command(company=company, command=command)


def fetch_my_greenhouse(company: dict[str, Any]) -> list[Job]:
    command = company.get("my_greenhouse_command") or MY_GREENHOUSE_DEFAULT_COMMAND
    env_updates = {
        "MY_GREENHOUSE_COMPANY_LABEL": company["name"],
    }
    return _run_job_command(company=company, command=command, env_updates=env_updates)


def fetch_jobs_for_source(company: dict[str, Any], source: str, session: requests.Session) -> list[Job]:
    if source == "greenhouse":
        return fetch_greenhouse(company, session)
    if source == "lever":
        return fetch_lever(company, session)
    if source in {"icims", "icms"}:
        return fetch_icims(company, session)
    if source == "generic":
        return fetch_generic(company, session)
    if source == "template":
        return fetch_template(company, session)
    if source in {"command", "playwright"}:
        return fetch_command(company)
    if source == "my_greenhouse":
        return fetch_my_greenhouse(company)
    raise CrawlFetchError(f"Unsupported source: {source}")
