#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
from typing import Any


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


def normalize_text(value: Any, max_len: int = 400) -> str:
    text = " ".join(str(value or "").split()).strip()
    if not text:
        return ""
    if len(text) <= max_len:
        return text
    return f"{text[:max_len].rstrip()}..."


def summarize_locations(values: list[Any]) -> str:
    cleaned: list[str] = []
    seen: set[str] = set()
    for raw in values:
        text = normalize_text(raw, 120).lstrip("/").strip()
        if not text:
            continue
        if re.fullmatch(r"\d+\s+locations?", text, flags=re.IGNORECASE):
            continue
        key = text.lower()
        if key in seen:
            continue
        seen.add(key)
        cleaned.append(text)
    if not cleaned:
        return ""
    if len(cleaned) <= 2:
        return normalize_text("; ".join(cleaned), 200)
    return normalize_text(f"{'; '.join(cleaned[:2])} +{len(cleaned) - 2} more", 200)


def extract_jobs_from_page(page: Any) -> list[dict[str, Any]]:
    rows = page.eval_on_selector_all(
        'a[href*="/en/jobs/jr"]',
        """
        (nodes) => nodes.map((node) => {
          const title = (node.textContent || "").trim();
          let card = node.parentElement;
          while (card) {
            const jobLinks = card.querySelectorAll('a[href*="/en/jobs/jr"]').length;
            const hasLocations = card.querySelectorAll("li").length > 0;
            if (jobLinks === 1 && hasLocations) {
              break;
            }
            card = card.parentElement;
          }
          if (!card) {
            card = node.closest("section, article, div") || node.parentElement;
          }
          const team = (card?.querySelector("p")?.textContent || "").trim();
          const locations = Array.from(card?.querySelectorAll("li") || [])
            .map((item) => (item.textContent || "").trim())
            .filter(Boolean);
          return {
            title,
            url: node.href || "",
            team,
            locations,
          };
        })
        """,
    )

    jobs: list[dict[str, Any]] = []
    seen_urls: set[str] = set()
    for row in rows:
        url = normalize_text((row or {}).get("url"), 900)
        title = normalize_text((row or {}).get("title"), 260)
        if not url or not title or url in seen_urls:
            continue
        seen_urls.add(url)
        match = re.search(r"/en/jobs/(jr[0-9]+)/", url, flags=re.IGNORECASE)
        jobs.append(
            {
                "external_id": match.group(1).lower() if match else "",
                "title": title,
                "url": url,
                "team": normalize_text((row or {}).get("team"), 140),
                "location": summarize_locations((row or {}).get("locations") or []),
            }
        )
    return jobs


def fetch_salesforce_jobs(
    careers_url: str,
    search_term: str,
    timeout_ms: int,
    wait_after_load_ms: int,
    max_pages: int,
    max_jobs: int,
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
            response = page.goto(careers_url, wait_until="domcontentloaded", timeout=timeout_ms)
            if response and response.status in {401, 403, 429}:
                browser.close()
                return ("blocked", f"HTTP {response.status} on initial page load", [])

            try:
                page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 15000))
            except PlaywrightTimeoutError:
                pass
            if wait_after_load_ms > 0:
                page.wait_for_timeout(wait_after_load_ms)

            if search_term:
                searchbox = page.get_by_role("searchbox", name="Keywords")
                searchbox.fill(search_term)
                page.get_by_role("button", name=re.compile(r"^(Search|Apply filters)$", re.IGNORECASE)).click()
                page.wait_for_selector('a[href*="/en/jobs/jr"]', timeout=timeout_ms)
                try:
                    page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 15000))
                except PlaywrightTimeoutError:
                    pass
                if wait_after_load_ms > 0:
                    page.wait_for_timeout(wait_after_load_ms)

            collected: list[dict[str, Any]] = []
            seen_urls: set[str] = set()
            seen_pages: set[str] = set()
            pages_visited = 0

            while len(collected) < max_jobs and pages_visited < max_pages:
                current_url = page.url
                if current_url in seen_pages:
                    break
                seen_pages.add(current_url)
                pages_visited += 1

                for job in extract_jobs_from_page(page):
                    url = normalize_text(job.get("url"), 900)
                    if not url or url in seen_urls:
                        continue
                    seen_urls.add(url)
                    collected.append(job)
                    if len(collected) >= max_jobs:
                        break
                if len(collected) >= max_jobs:
                    break

                next_url = page.eval_on_selector(
                    'a[aria-label="Next page"]',
                    '(node) => node ? (node.href || "") : ""',
                )
                next_url = normalize_text(next_url, 1200)
                if not next_url or next_url in seen_pages:
                    break

                page.goto(next_url, wait_until="domcontentloaded", timeout=timeout_ms)
                page.wait_for_selector('a[href*="/en/jobs/jr"]', timeout=timeout_ms)
                try:
                    page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 15000))
                except PlaywrightTimeoutError:
                    pass
                if wait_after_load_ms > 0:
                    page.wait_for_timeout(wait_after_load_ms)

            browser.close()
            if not collected:
                return ("error", "No Salesforce jobs found after applying search filters", [])
            return ("ok", f"Captured {len(collected)} Salesforce job(s) across {pages_visited} page(s)", collected)
    except Exception as exc:  # noqa: BLE001
        message = f"{type(exc).__name__}: {exc}"
        lowered = message.lower()
        if any(token in lowered for token in ("403", "429", "captcha", "challenge")):
            return ("blocked", message, [])
        return ("error", message, [])


def main() -> int:
    careers_url = (os.getenv("CAREERS_URL") or "https://careers.salesforce.com/en/jobs/").strip()
    search_term = (os.getenv("SALESFORCE_SEARCH") or "software").strip()
    timeout_ms = parse_int_env("SALESFORCE_TIMEOUT_MS", 70000, 5000)
    wait_after_load_ms = parse_int_env("SALESFORCE_WAIT_AFTER_LOAD_MS", 2500, 0)
    max_pages = parse_int_env("SALESFORCE_MAX_PAGES", 18, 1)
    max_jobs = parse_int_env("SALESFORCE_MAX_JOBS", 180, 1)
    headless = parse_bool_env("PLAYWRIGHT_HEADLESS", True)

    status, message, jobs = fetch_salesforce_jobs(
        careers_url=careers_url,
        search_term=search_term,
        timeout_ms=timeout_ms,
        wait_after_load_ms=wait_after_load_ms,
        max_pages=max_pages,
        max_jobs=max_jobs,
        headless=headless,
    )
    return emit(status, message, jobs)


if __name__ == "__main__":
    raise SystemExit(main())
