#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import sys
from typing import Any

import requests

DEFAULT_API_URL = "https://www-api.ibm.com/search/api/v2"
DEFAULT_QUERY = "hashicorp"


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


def normalize_location(value: str) -> str:
    location = normalize_text(value, 220)
    if not location:
        return ""
    return re.sub(r",\s*US\s*$", ", United States", location)


def is_us_location(value: str) -> bool:
    raw = normalize_location(value)
    location = raw.lower()
    if not raw:
        return False
    if "united states" in location or re.search(r"\busa?\b", location):
        return True

    # IBM uses trailing country codes (for example ", IN", ", AU", ", US").
    trailing_country = re.search(r",\s*([A-Z]{2})\s*$", raw)
    if trailing_country:
        return trailing_country.group(1).upper() == "US"

    if location.endswith(", us") or ", us," in location:
        return True
    if re.search(
        r",\s*(?:al|ak|az|ar|ca|co|ct|de|dc|fl|ga|hi|ia|id|il|in|ks|ky|la|ma|md|me|mi|mn|mo|ms|mt|nc|nd|ne|nh|nj|nm|nv|ny|oh|ok|or|pa|ri|sc|sd|tn|tx|ut|va|vt|wa|wi|wv|wy)\b",
        location,
    ):
        return True
    return False


def build_payload(query: str, size: int, offset: int) -> dict[str, Any]:
    return {
        "appId": "careers",
        "scopes": ["careers2"],
        "query": {
            "bool": {
                "must": [
                    {
                        "simple_query_string": {
                            "query": query,
                            "fields": [
                                "keywords^1",
                                "body^1",
                                "url^2",
                                "description^2",
                                "h1s_content^2",
                                "title^3",
                                "field_text_01",
                            ],
                        }
                    }
                ]
            }
        },
        "size": size,
        "from": offset,
        "sort": [{"_score": "desc"}, {"pageviews": "desc"}],
        "lang": "zz",
        "localeSelector": {},
        "sm": {"query": query, "lang": "zz"},
        "_source": [
            "_id",
            "title",
            "url",
            "description",
            "field_keyword_08",  # Team
            "field_keyword_17",  # Remote/Hybrid marker
            "field_keyword_18",  # Experience level
            "field_keyword_19",  # Location text
        ],
    }


def main() -> int:
    api_url = (os.getenv("HASHICORP_IBM_API_URL") or "").strip() or DEFAULT_API_URL
    query = (os.getenv("HASHICORP_QUERY") or "").strip() or DEFAULT_QUERY
    timeout_seconds = parse_int_env("HASHICORP_TIMEOUT_SECONDS", 35, minimum=5)
    page_size = parse_int_env("HASHICORP_PAGE_SIZE", 30, minimum=1, maximum=50)
    max_pages = parse_int_env("HASHICORP_MAX_PAGES", 8, minimum=1)
    max_jobs = parse_int_env("HASHICORP_MAX_JOBS", 300, minimum=1)
    us_only = parse_bool_env("HASHICORP_US_ONLY", True)

    session = requests.Session()
    jobs: list[dict[str, Any]] = []
    seen_urls: set[str] = set()
    total = None

    for page in range(max_pages):
        offset = page * page_size
        payload = build_payload(query=query, size=page_size, offset=offset)
        try:
            response = session.post(
                api_url,
                json=payload,
                timeout=timeout_seconds,
                headers={"Accept": "application/json"},
            )
        except requests.RequestException as exc:
            return emit("error", f"RequestException: {exc}")

        if response.status_code in {401, 403, 429}:
            return emit("blocked", f"HTTP {response.status_code} from IBM careers endpoint")
        if response.status_code >= 400:
            return emit("error", f"IBM careers API HTTP {response.status_code}")

        try:
            parsed = response.json()
        except ValueError:
            return emit("error", "IBM careers API returned invalid JSON")

        hits_block = parsed.get("hits")
        if not isinstance(hits_block, dict):
            return emit("error", "Unexpected IBM careers payload: missing hits object")

        if total is None:
            total_value = hits_block.get("total")
            if isinstance(total_value, dict):
                maybe_count = total_value.get("value")
                if isinstance(maybe_count, int):
                    total = maybe_count

        hit_rows = hits_block.get("hits")
        if not isinstance(hit_rows, list) or not hit_rows:
            break

        for row in hit_rows:
            if not isinstance(row, dict):
                continue
            source = row.get("_source")
            if not isinstance(source, dict):
                continue

            title = normalize_text(source.get("title"), 260)
            url = normalize_text(source.get("url"), 700)
            location = normalize_location(source.get("field_keyword_19"))
            team = normalize_text(source.get("field_keyword_08"), 140)
            experience = normalize_text(source.get("field_keyword_18"), 120)
            remote = normalize_text(source.get("field_keyword_17"), 120)
            description = normalize_text(source.get("description"), 2200)

            if not title or not url:
                continue
            if us_only and not is_us_location(location):
                continue
            if url in seen_urls:
                continue
            seen_urls.add(url)

            team_parts = [value for value in [team, experience, remote] if value]
            jobs.append(
                {
                    "external_id": normalize_text(row.get("_id"), 120) or None,
                    "title": title,
                    "url": url,
                    "location": location or None,
                    "team": normalize_text(" | ".join(team_parts), 220) or None,
                    "description": description or None,
                    # IBM API v2 does not expose posted timestamp in this scope response.
                    "posted_at": None,
                }
            )
            if len(jobs) >= max_jobs:
                break

        if len(jobs) >= max_jobs:
            break
        if total is not None and (offset + page_size) >= total:
            break

    return emit("ok", f"Extracted {len(jobs)} HashiCorp/IBM job(s)", jobs)


if __name__ == "__main__":
    sys.exit(main())
