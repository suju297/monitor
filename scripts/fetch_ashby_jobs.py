#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import sys
from typing import Any
from urllib.parse import urlparse

import requests

DEFAULT_TIMEOUT_SECONDS = 35
DEFAULT_MAX_JOBS = 800


def emit(status: str, message: str, jobs: list[dict[str, Any]] | None = None) -> int:
    payload = {"status": status, "message": message, "jobs": jobs or []}
    print(json.dumps(payload, ensure_ascii=False))
    return 0


def parse_bool_env(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def parse_int_env(name: str, default: int, minimum: int = 1) -> int:
    raw = (os.getenv(name, "") or "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    if value < minimum:
        return default
    return value


def normalize_text(value: Any, limit: int = 320) -> str:
    text = re.sub(r"\s+", " ", str(value or "")).strip()
    if not text:
        return ""
    return f"{text[:limit].rstrip()}..." if len(text) > limit else text


def parse_query_terms(raw: str) -> list[str]:
    text = raw.strip().lower()
    if not text:
        return []
    if "," in text:
        values = [item.strip() for item in text.split(",")]
    else:
        values = re.split(r"\s+", text)
    return [value for value in values if value]


def board_from_careers_url(raw_url: str) -> str:
    value = raw_url.strip()
    if not value:
        return ""
    parsed = urlparse(value)
    if not parsed.scheme:
        value = f"https://{value}"
        parsed = urlparse(value)
    host = parsed.netloc.lower()
    parts = [part for part in parsed.path.split("/") if part]
    if "api.ashbyhq.com" in host:
        if len(parts) >= 3 and parts[0] == "posting-api" and parts[1] == "job-board":
            return parts[2]
        return ""
    if "jobs.ashbyhq.com" in host:
        if parts:
            return parts[0]
        return ""
    return ""


def resolve_api_url(careers_url: str, board: str) -> str:
    if board:
        return f"https://api.ashbyhq.com/posting-api/job-board/{board}"
    value = careers_url.strip()
    if value:
        parsed = urlparse(value)
        if "api.ashbyhq.com" in parsed.netloc.lower():
            return value
    return ""


def format_address(payload: dict[str, Any]) -> str:
    postal = payload.get("postalAddress")
    if not isinstance(postal, dict):
        return ""
    parts = [
        normalize_text(postal.get("addressLocality"), 100),
        normalize_text(postal.get("addressRegion"), 100),
        normalize_text(postal.get("addressCountry"), 100),
    ]
    parts = [item for item in parts if item]
    return normalize_text(", ".join(parts), 180)


def pick_location(job: dict[str, Any]) -> str:
    primary = normalize_text(job.get("location"), 120)
    address = format_address(job.get("address") if isinstance(job.get("address"), dict) else {})
    if address:
        primary = address

    secondary_out: list[str] = []
    for item in job.get("secondaryLocations") or []:
        if not isinstance(item, dict):
            continue
        value = normalize_text(item.get("location"), 100)
        if not value:
            value = format_address(item.get("address") if isinstance(item.get("address"), dict) else {})
        if not value:
            continue
        if value not in secondary_out:
            secondary_out.append(value)

    if secondary_out:
        joined = ", ".join([primary] if primary else [])
        suffix = "; ".join(secondary_out[:3])
        if joined:
            return normalize_text(f"{joined}; Other: {suffix}", 220)
        return normalize_text(suffix, 220)
    return normalize_text(primary, 220)


def is_us_job(job: dict[str, Any], location: str) -> bool:
    haystacks = [
        location,
        normalize_text(job.get("location"), 120),
    ]
    address = job.get("address")
    if isinstance(address, dict):
        postal = address.get("postalAddress")
        if isinstance(postal, dict):
            haystacks.append(normalize_text(postal.get("addressCountry"), 80))
            haystacks.append(normalize_text(postal.get("addressRegion"), 80))
    for item in job.get("secondaryLocations") or []:
        if not isinstance(item, dict):
            continue
        haystacks.append(normalize_text(item.get("location"), 80))
        addr = item.get("address")
        if isinstance(addr, dict):
            postal = addr.get("postalAddress")
            if isinstance(postal, dict):
                haystacks.append(normalize_text(postal.get("addressCountry"), 80))
                haystacks.append(normalize_text(postal.get("addressRegion"), 80))

    combined = " ".join(haystacks).lower()
    if "united states" in combined or re.search(r"\busa\b", combined):
        return True
    if re.search(
        r",\s*(?:al|ak|az|ar|ca|co|ct|de|dc|fl|ga|hi|ia|id|il|in|ks|ky|la|ma|md|me|mi|mn|mo|ms|mt|nc|nd|ne|nh|nj|nm|nv|ny|oh|ok|or|pa|ri|sc|sd|tn|tx|ut|va|vt|wa|wi|wv|wy)\b",
        combined,
    ):
        return True
    return False


def matches_query(job: dict[str, Any], terms: list[str]) -> bool:
    if not terms:
        return True
    haystack = " ".join(
        [
            normalize_text(job.get("title"), 260),
            normalize_text(job.get("team"), 160),
            normalize_text(job.get("department"), 160),
            normalize_text(job.get("descriptionPlain"), 2200),
            normalize_text(job.get("jobUrl"), 500),
        ]
    ).lower()
    if not haystack:
        return False
    return any(term in haystack for term in terms)


def main() -> int:
    careers_url = (os.getenv("CAREERS_URL") or "").strip()
    board = (os.getenv("ASHBY_BOARD") or "").strip() or board_from_careers_url(careers_url)
    api_url = resolve_api_url(careers_url, board)
    if not api_url:
        return emit("error", "Unable to derive Ashby board/API URL from CAREERS_URL/ASHBY_BOARD")

    timeout_seconds = parse_int_env("ASHBY_TIMEOUT_SECONDS", DEFAULT_TIMEOUT_SECONDS, minimum=5)
    max_jobs = parse_int_env("ASHBY_MAX_JOBS", DEFAULT_MAX_JOBS, minimum=1)
    us_only = parse_bool_env("ASHBY_US_ONLY", True)
    query_terms = parse_query_terms(os.getenv("ASHBY_QUERY", ""))

    session = requests.Session()
    try:
        response = session.get(
            api_url,
            timeout=timeout_seconds,
            headers={"Accept": "application/json"},
        )
    except requests.RequestException as exc:
        return emit("error", f"RequestException: {exc}")

    if response.status_code in {401, 403, 429}:
        return emit("blocked", f"HTTP {response.status_code} from Ashby endpoint")
    if response.status_code >= 400:
        return emit("error", f"Ashby HTTP {response.status_code}")

    try:
        payload = response.json()
    except ValueError:
        return emit("error", "Ashby endpoint returned invalid JSON")

    rows = payload.get("jobs") if isinstance(payload, dict) else None
    if not isinstance(rows, list):
        return emit("error", "Unexpected Ashby payload: jobs is not a list")

    jobs: list[dict[str, Any]] = []
    seen_ids: set[str] = set()
    for item in rows:
        if not isinstance(item, dict):
            continue
        title = normalize_text(item.get("title"), 260)
        url = normalize_text(item.get("jobUrl") or item.get("applyUrl"), 700)
        external_id = normalize_text(item.get("id"), 120)
        if not title or not url:
            continue
        if external_id and external_id in seen_ids:
            continue

        location = pick_location(item)
        if us_only and not is_us_job(item, location):
            continue
        if not matches_query(item, query_terms):
            continue

        seen_ids.add(external_id or url)
        team = normalize_text(item.get("team"), 160)
        department = normalize_text(item.get("department"), 160)
        if team and department and department.lower() not in team.lower():
            team = normalize_text(f"{department} | {team}", 220)
        elif not team:
            team = department

        description = normalize_text(item.get("descriptionPlain"), 2200)
        jobs.append(
            {
                "external_id": external_id or None,
                "title": title,
                "url": url,
                "location": location or None,
                "team": team or None,
                "posted_at": normalize_text(item.get("publishedAt") or item.get("updatedAt"), 80) or None,
                "description": description or None,
            }
        )
        if len(jobs) >= max_jobs:
            break

    board_label = board or "unknown"
    return emit("ok", f"Extracted {len(jobs)} Ashby job(s) for board {board_label}", jobs)


if __name__ == "__main__":
    sys.exit(main())
