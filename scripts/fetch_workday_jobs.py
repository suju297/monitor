#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import sys
from typing import Any
from urllib.parse import urljoin

import requests


def emit(status: str, message: str, jobs: list[dict[str, Any]] | None = None) -> int:
    payload = {"status": status, "message": message, "jobs": jobs or []}
    print(json.dumps(payload, ensure_ascii=False))
    return 0


def parse_bool_env(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def parse_int_env(name: str, default: int, minimum: int = 1, maximum: int | None = None) -> int:
    raw = (os.getenv(name, "") or "").strip()
    if not raw:
        value = default
    else:
        try:
            value = int(raw)
        except ValueError:
            value = default
    if value < minimum:
        value = minimum
    if maximum is not None and value > maximum:
        value = maximum
    return value


def normalize_text(value: Any, limit: int = 260) -> str:
    text = re.sub(r"\s+", " ", str(value or "")).strip()
    if not text:
        return ""
    return f"{text[:limit].rstrip()}..." if len(text) > limit else text


def normalize_posted(value: Any) -> str:
    text = normalize_text(value, 120)
    if not text:
        return ""
    text = re.sub(r"(?i)^posted\s+", "", text).strip()
    text = re.sub(r"(?i)\b(\d+)\+\s+days?\s+ago\b", r"\1 days ago", text)
    text = re.sub(r"(?i)\b(\d+)\+\s+weeks?\s+ago\b", r"\1 weeks ago", text)
    text = re.sub(r"(?i)\b(\d+)\+\s+months?\s+ago\b", r"\1 months ago", text)
    text = re.sub(r"(?i)\b(\d+)\+\s+years?\s+ago\b", r"\1 years ago", text)
    return normalize_text(text, 80)


def parse_query_terms(raw: str) -> list[str]:
    text = raw.strip().lower()
    if not text:
        return []
    if "," in text:
        values = [item.strip() for item in text.split(",")]
    else:
        values = re.split(r"\s+", text)
    return [value for value in values if value]


def compile_optional_regex(name: str) -> re.Pattern[str] | None:
    pattern = (os.getenv(name) or "").strip()
    if not pattern:
        return None
    try:
        return re.compile(pattern, re.I)
    except re.error:
        return None


def is_us_location(value: str, external_path: str) -> bool:
    combined = " ".join([normalize_text(value, 220), normalize_text(external_path, 400)]).lower()
    if not combined:
        return False
    if "united states" in combined or re.search(r"\busa?\b", combined):
        return True
    if combined.startswith("us,") or ", us" in combined or "/job/us-" in combined:
        return True
    if re.search(
        r",\s*(?:al|ak|az|ar|ca|co|ct|de|dc|fl|ga|hi|ia|id|il|in|ks|ky|la|ma|md|me|mi|mn|mo|ms|mt|nc|nd|ne|nh|nj|nm|nv|ny|oh|ok|or|pa|ri|sc|sd|tn|tx|ut|va|vt|wa|wi|wv|wy)\b",
        combined,
    ):
        return True
    return False


def normalize_output_location(value: str, external_path: str, us_path_hint_re: re.Pattern[str] | None) -> str:
    location = normalize_text(value, 220)
    path = normalize_text(external_path, 400)
    if not location:
        location = ""
    if us_path_hint_re and us_path_hint_re.search(path):
        if "united states" not in location.lower() and not re.search(r"\busa?\b", location, re.I):
            if location:
                return normalize_text(f"{location}, United States", 220)
            return "United States"
    return location


def matches_query(title: str, location: str, external_path: str, terms: list[str]) -> bool:
    if not terms:
        return True
    haystack = " ".join([title, location, external_path]).lower()
    return any(term in haystack for term in terms)


def to_job(posting: dict[str, Any], careers_base: str, us_path_hint_re: re.Pattern[str] | None) -> dict[str, Any] | None:
    title = normalize_text(posting.get("title"), 260)
    external_path = normalize_text(posting.get("externalPath"), 600)
    if not title or not external_path:
        return None

    location = normalize_output_location(posting.get("locationsText"), external_path, us_path_hint_re)
    bullet_fields = posting.get("bulletFields")
    external_id = ""
    if isinstance(bullet_fields, list):
        for value in bullet_fields:
            token = normalize_text(value, 120)
            if token:
                external_id = token
                break

    return {
        "title": title,
        "url": urljoin(careers_base, external_path),
        "external_id": external_id or None,
        "location": location or None,
        "posted_at": normalize_posted(posting.get("postedOn")) or None,
        "external_path": external_path,
    }


def main() -> int:
    careers_url = (os.getenv("CAREERS_URL") or "").strip()
    api_url = (os.getenv("WORKDAY_API_URL") or "").strip()
    careers_base = (os.getenv("WORKDAY_CAREERS_BASE") or "").strip() or careers_url
    if not api_url:
        return emit("error", "WORKDAY_API_URL is required")
    if not careers_base:
        return emit("error", "WORKDAY_CAREERS_BASE or CAREERS_URL is required")

    timeout_seconds = parse_int_env("WORKDAY_TIMEOUT_SECONDS", 35, minimum=5)
    page_size = parse_int_env("WORKDAY_PAGE_SIZE", 20, minimum=1, maximum=20)
    max_pages = parse_int_env("WORKDAY_MAX_PAGES", 30, minimum=1)
    max_jobs = parse_int_env("WORKDAY_MAX_JOBS", 500, minimum=1)
    us_only = parse_bool_env("WORKDAY_US_ONLY", True)
    query_raw = os.getenv("WORKDAY_QUERY", "software")
    query_terms = parse_query_terms(query_raw)
    us_path_hint_re = compile_optional_regex("WORKDAY_US_PATH_HINT_REGEX")

    session = requests.Session()
    jobs: list[dict[str, Any]] = []
    seen_urls: set[str] = set()
    offset = 0
    total = None

    for _ in range(max_pages):
        body = {
            "limit": page_size,
            "offset": offset,
            "searchText": query_raw,
            "appliedFacets": {},
        }
        try:
            response = session.post(
                api_url,
                json=body,
                timeout=timeout_seconds,
                headers={"Accept": "application/json", "User-Agent": "Mozilla/5.0"},
            )
        except requests.RequestException as exc:
            return emit("error", f"RequestException: {exc}")

        if response.status_code in {401, 403, 429}:
            return emit("blocked", f"HTTP {response.status_code} from Workday endpoint")
        if response.status_code >= 400:
            return emit("error", f"Workday HTTP {response.status_code}")

        try:
            payload = response.json()
        except ValueError:
            return emit("error", "Workday endpoint returned invalid JSON")

        if total is None:
            maybe_total = payload.get("total")
            if isinstance(maybe_total, int):
                total = maybe_total

        postings = payload.get("jobPostings")
        if not isinstance(postings, list) or not postings:
            break

        page_added = 0
        for posting in postings:
            if not isinstance(posting, dict):
                continue
            job = to_job(posting, careers_base, us_path_hint_re)
            if not job:
                continue
            title = normalize_text(job.get("title"), 260)
            location = normalize_text(job.get("location"), 180)
            external_path = normalize_text(job.get("external_path"), 400)
            if us_only and not is_us_location(location, external_path):
                continue
            if not matches_query(title, location, external_path, query_terms):
                continue
            url = normalize_text(job.get("url"), 700)
            if not url or url in seen_urls:
                continue
            seen_urls.add(url)
            job.pop("external_path", None)
            jobs.append(job)
            page_added += 1
            if len(jobs) >= max_jobs:
                break

        if len(jobs) >= max_jobs:
            break
        if page_added == 0 and len(postings) < page_size:
            break

        offset += page_size
        if total is not None and offset >= total:
            break

    return emit("ok", f"Extracted {len(jobs)} Workday job(s)", jobs)


if __name__ == "__main__":
    sys.exit(main())
