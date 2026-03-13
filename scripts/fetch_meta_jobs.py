#!/usr/bin/env python3
from __future__ import annotations

import html
import json
import os
import re
import sys
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

CAREERS_BASE = "https://www.metacareers.com"
DETAIL_URL_PREFIX = f"{CAREERS_BASE}/profile/job_details/"

BLOCKED_TITLE_MARKERS = (
    "attention required",
    "just a moment",
    "are you a robot",
    "security checkpoint",
    "captcha",
    "access denied",
)

RESULT_QUERY_MARKERS = (
    "careersjobsearchresultsdataquery",
    "29615178951461218",
)


def emit(status: str, message: str, jobs: list[dict[str, Any]] | None = None) -> int:
    payload = {"status": status, "message": message, "jobs": jobs or []}
    print(json.dumps(payload, ensure_ascii=False))
    return 0


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


def parse_bool_env(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def normalize_text(value: Any, max_len: int = 2400) -> str:
    text = " ".join(str(value or "").split()).strip()
    if not text:
        return ""
    if len(text) <= max_len:
        return text
    return f"{text[:max_len].rstrip()}..."


def is_blocked_title(title: str) -> bool:
    lowered = (title or "").strip().lower()
    if not lowered:
        return False
    return any(marker in lowered for marker in BLOCKED_TITLE_MARKERS)


def summarize_list(values: list[Any], max_items: int = 3, max_len: int = 200) -> str:
    cleaned: list[str] = []
    seen: set[str] = set()
    for raw in values:
        text = normalize_text(raw, 120)
        if not text:
            continue
        key = text.lower()
        if key in seen:
            continue
        seen.add(key)
        cleaned.append(text)
    if not cleaned:
        return ""
    if len(cleaned) <= max_items:
        return normalize_text(", ".join(cleaned), max_len)
    remaining = len(cleaned) - max_items
    return normalize_text(f"{', '.join(cleaned[:max_items])} +{remaining} more", max_len)


def parse_jobs_from_graphql_payload(payload: dict[str, Any]) -> list[dict[str, Any]]:
    root = (
        payload.get("data", {})
        .get("job_search_with_featured_jobs", {})
    )
    if not isinstance(root, dict):
        return []

    output: list[dict[str, Any]] = []
    seen_ids: set[str] = set()
    for key in ("all_jobs", "featured_jobs"):
        rows = root.get(key)
        if not isinstance(rows, list):
            continue
        for row in rows:
            if not isinstance(row, dict):
                continue
            external_id = normalize_text(row.get("id"), 80)
            title = normalize_text(row.get("title"), 260)
            if not external_id or not title:
                continue
            if external_id in seen_ids:
                continue
            seen_ids.add(external_id)
            locations = row.get("locations")
            teams = row.get("teams")
            sub_teams = row.get("sub_teams")
            output.append(
                {
                    "external_id": external_id,
                    "title": title,
                    "url": f"{DETAIL_URL_PREFIX}{external_id}",
                    "location": summarize_list(locations if isinstance(locations, list) else []),
                    "team": summarize_list(
                        (teams if isinstance(teams, list) else [])
                        + (sub_teams if isinstance(sub_teams, list) else []),
                        max_items=4,
                    ),
                }
            )
    return output


def parse_jobs_from_page_fallback(page: Any) -> list[dict[str, Any]]:
    anchors = page.eval_on_selector_all(
        'a[href*="/profile/job_details/"]',
        """
        (nodes) => nodes.map((node) => ({
          href: node.href || "",
          text: (node.innerText || node.textContent || "").trim()
        }))
        """,
    )
    jobs: list[dict[str, Any]] = []
    seen_ids: set[str] = set()
    for item in anchors:
        href = normalize_text((item or {}).get("href"), 900)
        if not href:
            continue
        match = re.search(r"/profile/job_details/(\d+)", href)
        if not match:
            continue
        external_id = match.group(1)
        if external_id in seen_ids:
            continue
        seen_ids.add(external_id)
        raw_text = normalize_text((item or {}).get("text"), 800)
        title = raw_text.split("⋅", 1)[0].split("\n", 1)[0].strip() if raw_text else ""
        title = normalize_text(title, 260) or f"Meta job {external_id}"
        jobs.append(
            {
                "external_id": external_id,
                "title": title,
                "url": f"{DETAIL_URL_PREFIX}{external_id}",
                "location": "",
                "team": "",
            }
        )
    return jobs


def extract_jobs_with_playwright(
    careers_url: str,
    timeout_ms: int,
    wait_after_load_ms: int,
    headless: bool,
) -> tuple[str, str, list[dict[str, Any]]]:
    try:
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import sync_playwright
    except Exception:
        return (
            "error",
            "playwright is not installed. Install with: uv sync --extra playwright && uv run playwright install chromium",
            [],
        )

    try:
        with sync_playwright() as pw:
            browser = pw.chromium.launch(headless=headless)
            context = browser.new_context()
            page = context.new_page()

            captured_payloads: list[dict[str, Any]] = []

            def on_response(response: Any) -> None:
                if "/graphql" not in (response.url or ""):
                    return
                post_data = (response.request.post_data or "").lower()
                if not any(marker in post_data for marker in RESULT_QUERY_MARKERS):
                    return
                try:
                    payload = response.json()
                except Exception:
                    return
                if isinstance(payload, dict):
                    captured_payloads.append(payload)

            page.on("response", on_response)
            initial = page.goto(careers_url, wait_until="domcontentloaded", timeout=timeout_ms)
            if initial and initial.status in {401, 403, 429}:
                browser.close()
                return ("blocked", f"HTTP {initial.status} on initial page load", [])

            try:
                page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 20000))
            except PlaywrightTimeoutError:
                pass

            if wait_after_load_ms > 0:
                page.wait_for_timeout(wait_after_load_ms)

            if is_blocked_title(page.title() or ""):
                browser.close()
                return ("blocked", f"Challenge page detected ({page.title()})", [])

            jobs: list[dict[str, Any]] = []
            for payload in reversed(captured_payloads):
                jobs = parse_jobs_from_graphql_payload(payload)
                if jobs:
                    break

            if not jobs:
                jobs = parse_jobs_from_page_fallback(page)

            browser.close()
            if not jobs:
                return ("error", "No Meta jobs found in GraphQL response or page fallback", [])
            return ("ok", f"Captured {len(jobs)} Meta job(s)", jobs)
    except Exception as exc:  # noqa: BLE001
        message = f"{type(exc).__name__}: {exc}"
        lowered = message.lower()
        if any(token in lowered for token in ("403", "429", "captcha", "challenge")):
            return ("blocked", message, [])
        return ("error", message, [])


def extract_ld_json_job_payload(html_doc: str) -> dict[str, Any] | None:
    matches = re.findall(
        r'<script[^>]*type=["\']application/ld\+json["\'][^>]*>(.*?)</script>',
        html_doc,
        flags=re.IGNORECASE | re.DOTALL,
    )
    for raw in matches:
        snippet = raw.strip()
        if not snippet:
            continue
        try:
            parsed = json.loads(snippet)
        except json.JSONDecodeError:
            continue
        if isinstance(parsed, dict) and str(parsed.get("@type", "")).lower() == "jobposting":
            return parsed
    return None


def clean_job_text(value: Any, max_len: int = 4200) -> str:
    text = html.unescape(str(value or ""))
    text = re.sub(r"<[^>]+>", " ", text)
    text = normalize_text(text, max_len)
    return text


def parse_locations_from_ld_payload(payload: dict[str, Any]) -> str:
    raw = payload.get("jobLocation")
    if not isinstance(raw, list):
        return ""
    out: list[str] = []
    seen: set[str] = set()
    for item in raw:
        if not isinstance(item, dict):
            continue
        name = normalize_text(item.get("name"), 120)
        if not name:
            continue
        key = name.lower()
        if key in seen:
            continue
        seen.add(key)
        out.append(name)
    return summarize_list(out, max_items=4, max_len=220)


def fetch_job_details(url: str, timeout_seconds: int) -> dict[str, str]:
    try:
        request = Request(
            url=url,
            headers={
                "User-Agent": "Mozilla/5.0 (compatible; CareerMonitor/1.0)",
                "Accept": "text/html,application/xhtml+xml",
            },
        )
        with urlopen(request, timeout=timeout_seconds) as response:
            html_doc = response.read().decode("utf-8", errors="replace")
    except HTTPError:
        return {}
    except URLError:
        return {}
    except Exception:
        return {}

    payload = extract_ld_json_job_payload(html_doc)
    if not payload:
        return {}

    description_parts: list[str] = []
    for key in ("description", "responsibilities", "qualifications"):
        text = clean_job_text(payload.get(key), max_len=2600)
        if not text:
            continue
        description_parts.append(text)
    description = normalize_text(" ".join(description_parts), 4000)

    posted_at = normalize_text(payload.get("datePosted"), 120)
    location = parse_locations_from_ld_payload(payload)

    out: dict[str, str] = {}
    if description:
        out["description"] = description
    if posted_at:
        out["posted_at"] = posted_at
    if location:
        out["location"] = location
    return out


def main() -> int:
    careers_url = normalize_text(os.getenv("CAREERS_URL", ""), 1000)
    if not careers_url:
        return emit("error", "CAREERS_URL is not set")

    timeout_ms = parse_int_env("META_TIMEOUT_MS", 70000, minimum=5000)
    wait_after_load_ms = parse_int_env("META_WAIT_AFTER_LOAD_MS", 3500, minimum=0)
    max_jobs = parse_int_env("META_MAX_JOBS", 250, minimum=1)
    detail_fetch_limit = parse_int_env("META_DETAIL_FETCH_LIMIT", 36, minimum=0)
    detail_timeout_seconds = parse_int_env("META_DETAIL_TIMEOUT_SECONDS", 25, minimum=3)
    headless = parse_bool_env("META_HEADLESS", True)

    status, message, jobs = extract_jobs_with_playwright(
        careers_url=careers_url,
        timeout_ms=timeout_ms,
        wait_after_load_ms=wait_after_load_ms,
        headless=headless,
    )
    if status == "blocked":
        return emit("blocked", message)
    if status == "error":
        return emit("error", message)

    if max_jobs > 0 and len(jobs) > max_jobs:
        jobs = jobs[:max_jobs]

    enriched = 0
    if detail_fetch_limit > 0:
        for job in jobs:
            if enriched >= detail_fetch_limit:
                break
            details = fetch_job_details(job["url"], timeout_seconds=detail_timeout_seconds)
            if not details:
                continue
            for field in ("description", "posted_at"):
                value = normalize_text(details.get(field), 4000)
                if value:
                    job[field] = value
            if not job.get("location"):
                location = normalize_text(details.get("location"), 220)
                if location:
                    job["location"] = location
            enriched += 1

    return emit(
        "ok",
        f"{message}; returning {len(jobs)} job(s) with {enriched} detail enrichment(s)",
        jobs,
    )


if __name__ == "__main__":
    sys.exit(main())
