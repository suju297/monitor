#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import sys
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import urljoin
from urllib.request import Request, urlopen

API_URL = "https://zendesk.wd1.myworkdayjobs.com/wday/cxs/zendesk/zendesk/jobs"
CAREERS_BASE = "https://zendesk.wd1.myworkdayjobs.com/en-US/zendesk"
US_LOCATION_COUNTRY_ID = "bc33aa3152ec42d4995f4791a106ed09"


def emit(status: str, message: str, jobs: list[dict[str, Any]] | None = None) -> int:
    payload = {"status": status, "message": message, "jobs": jobs or []}
    print(json.dumps(payload, ensure_ascii=False))
    return 0


def parse_int_env(name: str, default: int) -> int:
    raw = (os.getenv(name, "") or "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return value if value > 0 else default


def normalize_text(value: Any, max_len: int = 240) -> str:
    text = " ".join(str(value or "").split()).strip()
    if not text:
        return ""
    return text[:max_len].strip()


def post_json(url: str, payload: dict[str, Any], timeout_s: int) -> dict[str, Any]:
    data = json.dumps(payload).encode("utf-8")
    req = Request(
        url=url,
        data=data,
        headers={"Content-Type": "application/json", "Accept": "application/json"},
        method="POST",
    )
    with urlopen(req, timeout=timeout_s) as resp:
        raw = resp.read().decode("utf-8", errors="replace")
    parsed = json.loads(raw)
    if not isinstance(parsed, dict):
        raise ValueError("Workday endpoint returned non-object JSON")
    return parsed


def posting_to_job(posting: dict[str, Any]) -> dict[str, Any] | None:
    title = normalize_text(posting.get("title"), 260)
    external_path = normalize_text(posting.get("externalPath"), 600)
    if not title or not external_path:
        return None
    location = normalize_text(posting.get("locationsText"), 160)
    posted_at = normalize_text(posting.get("postedOn"), 120)
    bullet_fields = posting.get("bulletFields")
    external_id = ""
    if isinstance(bullet_fields, list) and bullet_fields:
        external_id = normalize_text(bullet_fields[0], 120)

    # Workday may still return multi-location roles with non-US canonical paths.
    # Keep only roles explicitly marked US in path or location text.
    path_lower = external_path.lower()
    location_lower = location.lower()
    if "united-states-of-america" not in path_lower and "united states of america" not in location_lower:
        return None

    return {
        "title": title,
        "url": urljoin(CAREERS_BASE, external_path),
        "external_id": external_id or None,
        "location": location or None,
        "posted_at": posted_at or None,
    }


def main() -> int:
    timeout_s = parse_int_env("ZENDESK_WORKDAY_TIMEOUT_SECONDS", 35)
    page_size = min(parse_int_env("ZENDESK_WORKDAY_PAGE_SIZE", 20), 20)
    max_pages = parse_int_env("ZENDESK_WORKDAY_MAX_PAGES", 20)

    jobs: list[dict[str, Any]] = []
    seen_urls: set[str] = set()
    offset = 0
    total = None

    for _ in range(max_pages):
        body = {
            "limit": page_size,
            "offset": offset,
            "searchText": "",
            "appliedFacets": {"locationCountry": [US_LOCATION_COUNTRY_ID]},
        }
        try:
            payload = post_json(API_URL, body, timeout_s=timeout_s)
        except HTTPError as exc:
            if exc.code in {401, 403, 429}:
                return emit("blocked", f"HTTP {exc.code} from Zendesk Workday endpoint")
            return emit("error", f"HTTPError {exc.code}: {exc.reason}")
        except URLError as exc:
            return emit("error", f"URLError: {exc.reason}")
        except Exception as exc:  # noqa: BLE001
            return emit("error", f"{type(exc).__name__}: {exc}")

        if total is None:
            maybe_total = payload.get("total")
            if isinstance(maybe_total, int):
                total = maybe_total

        postings = payload.get("jobPostings")
        if not isinstance(postings, list) or not postings:
            break

        page_count = 0
        for item in postings:
            if not isinstance(item, dict):
                continue
            job = posting_to_job(item)
            if not job:
                continue
            url = job.get("url") or ""
            if not url or url in seen_urls:
                continue
            seen_urls.add(url)
            jobs.append(job)
            page_count += 1

        if page_count == 0 or len(postings) < page_size:
            break

        offset += page_size
        if total is not None and offset >= total:
            break

    return emit("ok", f"Extracted {len(jobs)} US job(s) from Zendesk Workday", jobs)


if __name__ == "__main__":
    sys.exit(main())
