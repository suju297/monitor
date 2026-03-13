#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import sys
from typing import Any

import requests

DEFAULT_API_URL = "https://careers.amd.com/api/jobs"


def emit(status: str, message: str, jobs: list[dict[str, Any]]) -> int:
    print(json.dumps({"status": status, "message": message, "jobs": jobs}, ensure_ascii=False))
    return 0


def parse_bool_env(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


def normalize_text(value: Any, limit: int = 900) -> str:
    text = re.sub(r"\s+", " ", str(value or "")).strip()
    if not text:
        return ""
    return f"{text[:limit].rstrip()}..." if len(text) > limit else text


def normalize_location(data: dict[str, Any]) -> str | None:
    preferred = normalize_text(data.get("full_location"), 220)
    if preferred:
        return preferred

    parts = [
        normalize_text(data.get("city"), 80),
        normalize_text(data.get("state"), 80),
        normalize_text(data.get("country"), 80),
    ]
    compact = ", ".join(part for part in parts if part)
    compact = normalize_text(compact, 220)
    return compact or None


def normalize_team(data: dict[str, Any]) -> str | None:
    raw = data.get("category")
    if isinstance(raw, list):
        values = [normalize_text(item, 60) for item in raw]
        values = [value for value in values if value]
        return " | ".join(values) if values else None
    value = normalize_text(raw, 120)
    return value or None


def parse_query_terms(query: str) -> list[str]:
    raw = query.strip().lower()
    if not raw:
        return []
    if "," in raw:
        terms = [item.strip() for item in raw.split(",")]
    else:
        terms = re.split(r"\s+", raw)
    return [term for term in terms if term]


def matches_query(data: dict[str, Any], terms: list[str]) -> bool:
    if not terms:
        return True
    haystack = " ".join(
        [
            normalize_text(data.get("title"), 300),
            normalize_text(data.get("description"), 1800),
            normalize_text(data.get("responsibilities"), 1200),
            normalize_text(data.get("qualifications"), 1200),
            normalize_text(data.get("category"), 220),
        ]
    ).lower()
    if not haystack:
        return False
    # Treat multi-word query as broad OR to avoid over-filtering.
    return any(term in haystack for term in terms)


def is_us_job(data: dict[str, Any], location: str | None) -> bool:
    country_code = normalize_text(data.get("country_code"), 16).upper()
    if country_code in {"US", "USA", "ISO-COUNTRY-USA"}:
        return True

    country = normalize_text(data.get("country"), 80).lower()
    if "united states" in country or country == "usa":
        return True

    location_text = normalize_text(location, 220).lower()
    if "united states" in location_text or re.search(r"\busa\b", location_text):
        return True
    if re.search(r",\s*[A-Z]{2}(?:\s|$)", location or ""):
        return True
    return False


def main() -> int:
    api_url = (os.getenv("CAREERS_URL") or "").strip() or DEFAULT_API_URL
    max_jobs = max(1, int(os.getenv("AMD_MAX_JOBS", "220")))
    page_size = max(10, min(200, int(os.getenv("AMD_PAGE_SIZE", "100"))))
    max_pages = max(1, int(os.getenv("AMD_MAX_PAGES", "25")))
    timeout_seconds = max(10, int(os.getenv("AMD_TIMEOUT_SECONDS", "30")))
    us_only = parse_bool_env("AMD_US_ONLY", True)
    query_terms = parse_query_terms(os.getenv("AMD_QUERY", ""))

    session = requests.Session()
    jobs: list[dict[str, Any]] = []
    seen_ids: set[str] = set()

    try:
        for page in range(1, max_pages + 1):
            response = session.get(
                api_url,
                params={"page": page, "limit": page_size},
                timeout=timeout_seconds,
            )
            if response.status_code in {401, 403, 429}:
                return emit("blocked", f"HTTP {response.status_code} from AMD API", [])
            if response.status_code >= 400:
                return emit("error", f"AMD API HTTP {response.status_code}", [])

            payload = response.json()
            rows = payload.get("jobs", []) if isinstance(payload, dict) else []
            if not isinstance(rows, list):
                return emit("error", "Unexpected AMD API format: jobs is not a list", [])
            if not rows:
                break

            for row in rows:
                if not isinstance(row, dict):
                    continue
                data = row.get("data") if isinstance(row.get("data"), dict) else row
                if not isinstance(data, dict):
                    continue

                title = normalize_text(data.get("title"), 260)
                url = normalize_text(data.get("apply_url"), 600)
                req_id = normalize_text(data.get("req_id") or data.get("slug"), 80)
                if not title or not url:
                    continue

                dedupe_key = req_id or url
                if dedupe_key in seen_ids:
                    continue

                location = normalize_location(data)
                if us_only and not is_us_job(data, location):
                    continue
                if not matches_query(data, query_terms):
                    continue

                seen_ids.add(dedupe_key)
                jobs.append(
                    {
                        "external_id": req_id or None,
                        "title": title,
                        "url": url,
                        "location": location,
                        "team": normalize_team(data),
                        "posted_at": normalize_text(data.get("posted_date"), 80) or None,
                    }
                )
                if len(jobs) >= max_jobs:
                    break

            if len(jobs) >= max_jobs or len(rows) < page_size:
                break

        return emit("ok", f"Extracted {len(jobs)} AMD API job(s)", jobs)
    except requests.RequestException as exc:
        message = f"RequestException: {exc}"
        if any(token in message.lower() for token in ["403", "429", "forbidden", "captcha", "blocked"]):
            return emit("blocked", message, [])
        return emit("error", message, [])
    except Exception as exc:  # noqa: BLE001
        return emit("error", f"{type(exc).__name__}: {exc}", [])


if __name__ == "__main__":
    sys.exit(main())
