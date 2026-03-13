#!/usr/bin/env python3
from __future__ import annotations

import html
import json
import os
import re
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urljoin, urlparse
from urllib.request import Request, urlopen

from career_monitor.local_paths import prefer_legacy_or_local

BLOCKED_TITLE_MARKERS = (
    "attention required",
    "just a moment",
    "are you a robot",
    "security checkpoint",
    "captcha",
    "access denied",
)

EMAIL_SELECTORS = (
    "input[type='email']",
    "input[name='email']",
    "input[name*='email' i]",
    "input[id*='email' i]",
    "input[autocomplete='username']",
)

PASSWORD_SELECTORS = (
    "input[type='password']",
    "input[name='password']",
    "input[name*='password' i]",
    "input[id*='password' i]",
    "input[autocomplete='current-password']",
)

CODE_INPUT_SELECTORS = (
    "input[id*='otp' i]",
    "input[name*='otp' i]",
    "input[id*='code' i]",
    "input[name*='code' i]",
    "input[inputmode='numeric']",
    "input[maxlength='1']",
)

SUBMIT_SELECTORS = (
    "button[type='submit']",
    "input[type='submit']",
    "button:has-text('Sign in')",
    "button:has-text('Log in')",
    "button:has-text('Continue')",
    "button:has-text('Next')",
    "button:has-text('Send security code')",
    "button:has-text('Submit')",
)


def emit(status: str, message: str, jobs: list[dict] | None = None) -> int:
    payload = {
        "status": status,
        "message": message,
        "jobs": jobs or [],
    }
    print(json.dumps(payload, ensure_ascii=False))
    return 0


def truthy_env(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


def sanitized_secret(name: str) -> str:
    value = (os.getenv(name, "") or "").strip()
    if not value:
        return ""
    if value.upper().startswith("REPLACE_"):
        return ""
    return value


def clean_security_code(value: str) -> str:
    return "".join(ch for ch in (value or "") if ch.isalnum())


def first_visible_selector(page, selectors: tuple[str, ...]) -> str | None:
    for selector in selectors:
        try:
            locator = page.locator(selector).first
            if locator.count() > 0 and locator.is_visible(timeout=500):
                return selector
        except Exception:
            continue
    return None


def visible_code_inputs(page) -> list:
    candidates = []
    try:
        locator = page.locator("input[type='text']")
        total = locator.count()
    except Exception:
        return candidates
    for idx in range(total):
        input_locator = locator.nth(idx)
        try:
            if not input_locator.is_visible(timeout=300):
                continue
            candidates.append(input_locator)
        except Exception:
            continue
    return candidates


def has_login_form(page) -> bool:
    email_selector = first_visible_selector(page, EMAIL_SELECTORS)
    password_selector = first_visible_selector(page, PASSWORD_SELECTORS)
    if email_selector and password_selector:
        return True
    if email_selector:
        return True
    code_selector = first_visible_selector(page, CODE_INPUT_SELECTORS)
    if code_selector:
        return True
    if is_security_code_step(page):
        return True
    url = (page.url or "").lower()
    if "sign_in" in url or "signin" in url or "login" in url:
        return True
    return False


def blocked_message(page, response) -> str:
    if response is not None and response.status in {401, 403, 429}:
        return f"HTTP {response.status} on page load"
    title = (page.title() or "").strip().lower()
    if any(marker in title for marker in BLOCKED_TITLE_MARKERS):
        return f"Challenge page detected ({title})"
    body = (page.content() or "").lower()
    if "captcha" in body or "verify you are human" in body:
        return "Captcha or human verification challenge detected"
    return ""


def is_security_code_step(page) -> bool:
    try:
        body = (page.content() or "").lower()
    except Exception:
        return False
    if "security code" not in body:
        return False
    if len(visible_code_inputs(page)) >= 4:
        return True
    return False


def security_code_cooldown_hint(page) -> str:
    try:
        body = " ".join((page.locator("body").inner_text(timeout=1200) or "").split()).lower()
    except Exception:
        return ""
    if "after 10 minutes, go back to get a new code" in body:
        return "Greenhouse code resend is rate-limited (10 minutes)."
    return ""


def enter_security_code(page, security_code: str, timeout_ms: int) -> bool:
    code = clean_security_code(security_code)
    if not code:
        return False
    inputs = visible_code_inputs(page)
    if not inputs:
        return False
    if len(inputs) == 1:
        try:
            inputs[0].fill(code, timeout=timeout_ms)
            return True
        except Exception:
            return False
    for idx, locator in enumerate(inputs):
        if idx >= len(code):
            break
        try:
            locator.fill(code[idx], timeout=timeout_ms)
        except Exception:
            return False
    return True


def title_from_url(url: str) -> str:
    parsed = urlparse(url)
    path_parts = [part for part in parsed.path.split("/") if part]
    if not path_parts:
        return "Greenhouse Job"
    raw = path_parts[-1]
    cleaned = re.sub(r"[-_]+", " ", raw).strip()
    cleaned = re.sub(r"\s+", " ", cleaned)
    return cleaned[:150] or "Greenhouse Job"


def extract_external_id(url: str) -> str | None:
    parsed = urlparse(url)
    query = parse_qs(parsed.query)
    gh_jid = "".join(query.get("gh_jid", [])).strip()
    if gh_jid:
        return gh_jid
    match = re.search(r"/jobs/([^/?#]+)", url)
    if not match:
        return None
    return match.group(1).strip() or None


def parse_relative_posted_label(value: str) -> str | None:
    text = " ".join((value or "").strip().lower().split())
    if not text:
        return None
    if text.startswith("posted "):
        text = text[len("posted ") :].strip()
    if not text:
        return None

    now = datetime.now(timezone.utc).replace(microsecond=0)
    delta: timedelta | None = None
    if text == "today":
        delta = timedelta(0)
    elif text == "yesterday":
        delta = timedelta(days=1)
    elif text == "last week":
        delta = timedelta(days=7)
    else:
        match = re.search(r"(\d+)\s+(minute|minutes|hour|hours|day|days|week|weeks)\b", text)
        if match:
            count = int(match.group(1))
            unit = match.group(2)
            if unit.startswith("minute"):
                delta = timedelta(minutes=count)
            elif unit.startswith("hour"):
                delta = timedelta(hours=count)
            elif unit.startswith("day"):
                delta = timedelta(days=count)
            elif unit.startswith("week"):
                delta = timedelta(days=7 * count)
    if delta is None:
        return None
    return (now - delta).isoformat()


def extract_location(context: str) -> str | None:
    text = context.strip()
    if not text:
        return None
    remote_match = re.search(r"\bremote\b(?:\s*-\s*[a-z]{2})?", text, flags=re.IGNORECASE)
    if remote_match:
        return remote_match.group(0).strip()
    comma_match = re.search(
        r"\b([A-Z][A-Za-z .'-]+,\s*(?:[A-Z]{2}|[A-Za-z][A-Za-z .'-]+))\b",
        text,
    )
    if comma_match:
        return comma_match.group(1).strip()
    return None


def filter_matches(title: str, context: str, query: str, location_filter: str, location: str) -> bool:
    normalized_title = " ".join((title or "").strip().lower().split())
    normalized_context = " ".join((context or "").strip().lower().split())
    normalized_location = " ".join((location or "").strip().lower().split())
    combined = (
        normalized_title
        if normalized_title and not is_generic_job_title(title)
        else " ".join(part for part in (normalized_title, normalized_context) if part)
    )
    if query:
        tokens = [token for token in re.split(r"[^a-z0-9+#]+", query.lower()) if token]
        expanded_tokens: set[str] = set(tokens)
        if {"software", "engineer"} & expanded_tokens or "engineering" in expanded_tokens:
            expanded_tokens.update(
                {
                    "software",
                    "engineer",
                    "engineering",
                    "developer",
                    "backend",
                    "frontend",
                    "front-end",
                    "fullstack",
                    "full-stack",
                    "platform",
                    "swe",
                    "devops",
                    "sre",
                }
            )
        if "swe" in expanded_tokens:
            expanded_tokens.update({"software", "engineer", "engineering"})
        if tokens:
            if not any(token in combined for token in expanded_tokens):
                return False
    if location_filter:
        location_haystack = " ".join(part for part in (normalized_location, normalized_context) if part)
        if location_filter.lower() not in location_haystack:
            return False
    return True


def first_nonempty(*values: str | None) -> str | None:
    for value in values:
        text = (value or "").strip()
        if text:
            return text
    return None


def is_generic_job_title(title: str) -> bool:
    normalized = " ".join((title or "").strip().lower().split())
    return normalized in {"view job", "job details", "details", "apply", "open", "opening", ""}


def strip_tags(raw_html: str) -> str:
    text = re.sub(r"<[^>]+>", " ", raw_html or "")
    return " ".join(html.unescape(text).split()).strip()


def extract_result_cards(page) -> list[dict]:
    return page.eval_on_selector_all(
        "[data-provides='search-result']",
        """
        (cards) => cards
          .map((card) => {
            const link =
              card.querySelector("a[href*='gh_src=my.greenhouse.search']") ||
              card.querySelector("a[href*='/jobs/']");
            if (!link) {
              return null;
            }

            const href = link.getAttribute('href') || link.href || '';
            const titleNode = card.querySelector('h1, h2, h3, h4, h5');
            const titleText = titleNode
              ? ((titleNode.getAttribute('title') || titleNode.textContent || '').trim())
              : '';
            const titleBlock = titleNode && titleNode.parentElement ? titleNode.parentElement : null;

            let companyText = '';
            const logo = card.querySelector('img.company-logo__logo[alt], img[alt]');
            if (logo) {
              companyText = (logo.getAttribute('alt') || '').trim();
            }
            if (!companyText && titleBlock) {
              const companyNode = titleBlock.querySelector('p');
              companyText = companyNode ? (companyNode.textContent || '').trim() : '';
            }

            const tagTexts = Array.from(card.querySelectorAll('.tag-text, .tag-content'))
              .map((el) => (el.textContent || '').trim())
              .filter(Boolean);
            const locationText =
              tagTexts.find((tag) => !/[€$£¥]|salary|compensation/i.test(tag)) || '';
            const cardText = (card.textContent || '').trim();
            const postedText = (
              Array.from(card.querySelectorAll('p, span, div'))
                .map((el) => (el.textContent || '').trim())
                .find((text) =>
                  /^(posted\\s+)?(?:today|yesterday|last week|\\d+\\s+(?:minute|minutes|hour|hours|day|days|week|weeks)\\s+ago)$/i.test(
                    text,
                  ),
                ) || ''
            );

            return {
              href,
              titleText,
              companyText,
              locationText,
              postedText,
              context: cardText.slice(0, 1000),
            };
          })
          .filter(Boolean)
        """,
    )


def extract_fallback_links(page) -> list[dict]:
    return page.eval_on_selector_all(
        "a[href]",
        """
        (nodes) => nodes.map((node) => {
          const href = node.getAttribute('href') || node.href || '';
          const text = (node.textContent || '').trim();
          const title = node.getAttribute('title') || node.getAttribute('aria-label') || '';
          const card =
            node.closest("[data-provides='search-result']") ||
            node.closest("article, li, [role='listitem'], div.rounded-lg, div[class*='rounded-lg']");
          const context = card ? (card.textContent || '').trim().slice(0, 1000) : '';
          const titleNode = card ? card.querySelector('h1, h2, h3, h4, h5') : null;
          const titleText = titleNode
            ? ((titleNode.getAttribute('title') || titleNode.textContent || '').trim())
            : '';
          const logo = card ? card.querySelector('img.company-logo__logo[alt], img[alt]') : null;
          const titleBlock = titleNode && titleNode.parentElement ? titleNode.parentElement : null;
          let companyText = logo ? (logo.getAttribute('alt') || '').trim() : '';
          if (!companyText && titleBlock) {
            const companyNode = titleBlock.querySelector('p');
            companyText = companyNode ? (companyNode.textContent || '').trim() : '';
          }
          const tagTexts = card
            ? Array.from(card.querySelectorAll('.tag-text, .tag-content'))
                .map((el) => (el.textContent || '').trim())
                .filter(Boolean)
            : [];
          const locationText = tagTexts.find((tag) => !/[€$£¥]|salary|compensation/i.test(tag)) || '';
          const postedText = (
            card
              ? Array.from(card.querySelectorAll('p, span, div'))
                  .map((el) => (el.textContent || '').trim())
                  .find((value) =>
                    /^(posted\\s+)?(?:today|yesterday|last week|\\d+\\s+(?:minute|minutes|hour|hours|day|days|week|weeks)\\s+ago)$/i.test(
                      value,
                    ),
                  ) || ''
              : ''
          );
          return { href, text, title, titleText, companyText, locationText, postedText, context };
        })
        """,
    )


def collect_jobs_from_links(raw_links: list[dict], careers_url: str, max_jobs: int) -> list[dict]:
    jobs: list[dict] = []
    seen_urls: set[str] = set()
    for item in raw_links:
        href = str((item or {}).get("href", "")).strip()
        if not href:
            continue
        absolute_url = urljoin(careers_url, href)
        parsed = urlparse(absolute_url)
        if parsed.scheme not in {"http", "https"}:
            continue
        if "/jobs/" not in parsed.path.lower():
            continue
        if absolute_url in seen_urls:
            continue
        seen_urls.add(absolute_url)

        raw_title = (
            str((item or {}).get("titleText", "")).strip()
            or str((item or {}).get("title", "")).strip()
            or str((item or {}).get("text", "")).strip()
        )
        raw_company = str((item or {}).get("companyText", "")).strip()
        raw_location = str((item or {}).get("locationText", "")).strip()
        posted_label = str((item or {}).get("postedText", "")).strip()
        context_text = str((item or {}).get("context", "")).strip()
        title = re.sub(r"\s+", " ", raw_title)[:220] or title_from_url(absolute_url)

        jobs.append(
            {
                "title": title,
                "url": absolute_url,
                "external_id": extract_external_id(absolute_url),
                "location": first_nonempty(raw_location, extract_location(context_text)),
                "team": raw_company or None,
                "posted_at": parse_relative_posted_label(posted_label),
                "_context": context_text,
            }
        )
        if len(jobs) >= max_jobs:
            break
    return jobs


def enrich_jobs(jobs: list[dict], timeout_ms: int, detail_fetch_limit: int) -> None:
    enriched = 0
    for job in jobs:
        if enriched >= detail_fetch_limit:
            break
        needs_title = is_generic_job_title(str(job.get("title") or ""))
        needs_location = not str(job.get("location") or "").strip()
        needs_team = not str(job.get("team") or "").strip()
        needs_posted_at = not str(job.get("posted_at") or "").strip()
        board_detail = fetch_greenhouse_board_job(str(job.get("url", "")), timeout_ms=timeout_ms)
        if board_detail.get("title"):
            job["title"] = board_detail["title"]
            needs_title = False
        if board_detail.get("location"):
            job["location"] = board_detail["location"]
            needs_location = False
        if board_detail.get("team"):
            job["team"] = board_detail["team"]
            needs_team = False
        if board_detail.get("posted_at"):
            job["posted_at"] = board_detail["posted_at"]
            needs_posted_at = False
        if board_detail.get("context"):
            job["_detail_context"] = board_detail["context"]
        if not needs_title and not needs_location and not needs_team and not needs_posted_at:
            enriched += 1
            continue

        detail = fetch_job_detail(str(job.get("url", "")), timeout_ms=timeout_ms)
        if needs_title and detail.get("title"):
            job["title"] = detail["title"]
        if needs_location and detail.get("location"):
            job["location"] = detail["location"]
        enriched += 1


def filter_jobs(jobs: list[dict], query: str, location_filter: str) -> list[dict]:
    filtered_jobs: list[dict] = []
    for job in jobs:
        if is_generic_job_title(str(job.get("title") or "")):
            continue
        if not filter_matches(
            title=str(job.get("title", "")),
            context=str(job.get("_detail_context") or job.get("_context") or ""),
            query=query,
            location_filter=location_filter,
            location=str(job.get("location", "")),
        ):
            continue
        for key in ("_context", "_detail_context"):
            if key in job:
                del job[key]
        filtered_jobs.append(job)
    return filtered_jobs


def fetch_job_detail(url: str, timeout_ms: int) -> dict[str, str]:
    timeout_s = max(5, int(timeout_ms / 1000))
    try:
        req = Request(
            url,
            headers={
                "User-Agent": "Mozilla/5.0 (compatible; CareerMonitor/1.0)",
                "Accept": "text/html,application/xhtml+xml",
            },
        )
        with urlopen(req, timeout=timeout_s) as resp:
            payload = resp.read().decode("utf-8", errors="replace")
    except (HTTPError, URLError, TimeoutError, ValueError):
        return {}
    except Exception:
        return {}

    out: dict[str, str] = {}
    title_match = re.search(r"<h1[^>]*>(.*?)</h1>", payload, flags=re.IGNORECASE | re.DOTALL)
    if title_match:
        title = strip_tags(title_match.group(1))
        if title:
            out["title"] = title[:220]

    # Best-effort location extraction from page text.
    text = strip_tags(payload)
    location_match = re.search(r"\b([A-Z][A-Za-z .'-]+,\s*(?:[A-Z]{2}|[A-Za-z][A-Za-z .'-]+))\b", text)
    if location_match:
        location = location_match.group(1).strip()[:120]
        lowered = location.lower()
        if "job application for" not in lowered and " at " not in lowered:
            out["location"] = location
    elif re.search(r"\bremote\b", text, flags=re.IGNORECASE):
        out["location"] = "Remote"

    return out


def parse_greenhouse_board_job(url: str) -> tuple[str, str] | None:
    parsed = urlparse(url)
    host = parsed.netloc.lower()
    if "greenhouse.io" not in host:
        return None
    if "job-boards." not in host and host != "boards.greenhouse.io":
        return None
    parts = [part for part in parsed.path.split("/") if part]
    if len(parts) < 3:
        return None
    if parts[1].lower() != "jobs":
        return None
    board = parts[0].strip()
    job_id = parts[2].strip()
    if not board or not job_id:
        return None
    return board, job_id


def fetch_greenhouse_board_job(url: str, timeout_ms: int) -> dict[str, str]:
    parsed = parse_greenhouse_board_job(url)
    if not parsed:
        return {}
    board, job_id = parsed
    timeout_s = max(5, int(timeout_ms / 1000))
    api_url = f"https://boards-api.greenhouse.io/v1/boards/{board}/jobs/{job_id}?content=true"
    try:
        req = Request(
            api_url,
            headers={
                "User-Agent": "Mozilla/5.0 (compatible; CareerMonitor/1.0)",
                "Accept": "application/json",
            },
        )
        with urlopen(req, timeout=timeout_s) as resp:
            payload = json.loads(resp.read().decode("utf-8", errors="replace"))
    except (HTTPError, URLError, TimeoutError, ValueError, json.JSONDecodeError):
        return {}
    except Exception:
        return {}

    out: dict[str, str] = {}
    title = str(payload.get("title", "")).strip()
    if title:
        out["title"] = title[:220]
    location = str((payload.get("location") or {}).get("name", "")).strip()
    if location:
        out["location"] = location[:120]
    first_published = str(payload.get("first_published", "")).strip()
    updated_at = str(payload.get("updated_at", "")).strip()
    if first_published:
        out["posted_at"] = first_published
    elif updated_at:
        out["posted_at"] = updated_at
    company_name = str(payload.get("company_name", "")).strip()
    if company_name:
        out["team"] = company_name[:140]
    content = strip_tags(str(payload.get("content") or ""))
    if content:
        out["context"] = content[:5000]
    return out


def click_submit(page, timeout_ms: int, prefer: tuple[str, ...] = ()) -> bool:
    ordered = list(prefer) + [selector for selector in SUBMIT_SELECTORS if selector not in prefer]
    for selector in ordered:
        try:
            locator = page.locator(selector).first
            if locator.count() == 0:
                continue
            if not locator.is_visible(timeout=700):
                continue
            locator.click(timeout=timeout_ms)
            return True
        except Exception:
            continue
    return False


def load_more_jobs(page, timeout_ms: int, max_pages: int, max_jobs: int) -> None:
    if max_pages <= 0:
        return

    last_visible_job_count = 0
    stagnant_clicks = 0
    for _ in range(max_pages):
        try:
            current_count = page.locator("a[href*='gh_src=my.greenhouse.search'], a[href*='/jobs/']").count()
        except Exception:
            current_count = 0

        if current_count >= max_jobs:
            break

        see_more = None
        for locator in (
            page.get_by_role("button", name=re.compile(r"see more jobs", flags=re.IGNORECASE)).first,
            page.get_by_role("link", name=re.compile(r"see more jobs", flags=re.IGNORECASE)).first,
            page.locator("button:has-text('See more jobs')").first,
            page.locator("a:has-text('See more jobs')").first,
            page.locator("text=/see more jobs/i").first,
        ):
            try:
                if locator.count() > 0:
                    see_more = locator
                    break
            except Exception:
                continue
        if see_more is None:
            break

        try:
            see_more.scroll_into_view_if_needed(timeout=timeout_ms)
            see_more.click(timeout=timeout_ms)
            try:
                page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 6000))
            except Exception:
                pass
            page.wait_for_timeout(900)
        except Exception:
            break

        # Let incremental list content render.
        for _ in range(3):
            page.mouse.wheel(0, 1200)
            page.wait_for_timeout(250)

        try:
            new_count = page.locator("a[href*='gh_src=my.greenhouse.search'], a[href*='/jobs/']").count()
        except Exception:
            new_count = current_count

        if new_count <= max(last_visible_job_count, current_count):
            stagnant_clicks += 1
            if stagnant_clicks >= 2:
                break
        else:
            stagnant_clicks = 0
            last_visible_job_count = new_count


def is_invalid_credentials(page) -> bool:
    try:
        body = (page.content() or "").lower()
    except Exception:
        return False
    return "invalid email or password" in body


def wait_for_manual_verification(page, timeout_ms: int) -> tuple[bool, str]:
    loops = max(1, timeout_ms // 500)
    for _ in range(loops):
        page.wait_for_timeout(500)
        if blocked_message(page, None):
            return False, blocked_message(page, None)
        if not has_login_form(page):
            return True, "Manual verification completed."
    return False, "Timed out waiting for manual verification to complete."


def perform_login(
    page,
    email: str,
    password: str,
    security_code: str,
    timeout_ms: int,
    allow_manual_auth: bool,
    manual_auth_timeout_ms: int,
) -> tuple[bool, str]:
    email_selector = first_visible_selector(page, EMAIL_SELECTORS)
    password_selector = first_visible_selector(page, PASSWORD_SELECTORS)
    if not email_selector and not password_selector:
        return False, "Login form fields were not found."

    if email_selector:
        try:
            page.fill(email_selector, email, timeout=timeout_ms)
        except Exception:
            return False, "Unable to fill Greenhouse email field."
        if not password_selector:
            if not click_submit(
                page,
                timeout_ms=timeout_ms,
                prefer=(
                    "button:has-text('Next')",
                    "button:has-text('Send security code')",
                    "button:has-text('Continue')",
                ),
            ):
                try:
                    page.press(email_selector, "Enter")
                except Exception:
                    return False, "Could not submit Greenhouse email step."
            page.wait_for_timeout(1200)
            if is_security_code_step(page):
                if enter_security_code(page, security_code=security_code, timeout_ms=timeout_ms):
                    if not click_submit(
                        page,
                        timeout_ms=timeout_ms,
                        prefer=("button:has-text('Submit')", "button[type='submit']"),
                    ):
                        pass
                    page.wait_for_timeout(800)
                    password_selector = first_visible_selector(page, PASSWORD_SELECTORS)
                if allow_manual_auth:
                    return wait_for_manual_verification(page, timeout_ms=manual_auth_timeout_ms)
                return (
                    False,
                    "Greenhouse requested an emailed security code. "
                    + (
                        security_code_cooldown_hint(page) + " "
                        if security_code_cooldown_hint(page)
                        else ""
                    )
                    + "Set MY_GREENHOUSE_SECURITY_CODE or run once with "
                    "MY_GREENHOUSE_LOGIN_HEADLESS=false MY_GREENHOUSE_ALLOW_MANUAL_AUTH=true, "
                    "complete the code step in the opened browser, then rerun headless.",
                )

    password_selector = first_visible_selector(page, PASSWORD_SELECTORS)
    if password_selector:
        try:
            page.fill(password_selector, password, timeout=timeout_ms)
        except Exception:
            return False, "Unable to fill Greenhouse password field."
        if not click_submit(
            page,
            timeout_ms=timeout_ms,
            prefer=("button:has-text('Sign in')", "button:has-text('Submit')"),
        ):
            try:
                page.press(password_selector, "Enter")
            except Exception:
                return False, "Could not submit Greenhouse password step."
    else:
        code_selector = first_visible_selector(page, CODE_INPUT_SELECTORS)
        if code_selector:
            if allow_manual_auth:
                return wait_for_manual_verification(page, timeout_ms=manual_auth_timeout_ms)
            return (
                False,
                "Greenhouse requested a security code. Run with MY_GREENHOUSE_LOGIN_HEADLESS=false and "
                "MY_GREENHOUSE_ALLOW_MANUAL_AUTH=true once to complete login and save session state.",
            )

    for _ in range(120):
        page.wait_for_timeout(500)
        if blocked_message(page, None):
            return False, blocked_message(page, None)
        if not has_login_form(page):
            return True, "Login successful."
        if is_security_code_step(page):
            if enter_security_code(page, security_code=security_code, timeout_ms=timeout_ms):
                if not click_submit(
                    page,
                    timeout_ms=timeout_ms,
                    prefer=("button:has-text('Submit')", "button[type='submit']"),
                ):
                    pass
                page.wait_for_timeout(800)
                continue
            if allow_manual_auth:
                return wait_for_manual_verification(page, timeout_ms=manual_auth_timeout_ms)
            return (
                False,
                "Greenhouse login requires an emailed security code. "
                + (
                    security_code_cooldown_hint(page) + " "
                    if security_code_cooldown_hint(page)
                    else ""
                )
                + "Set MY_GREENHOUSE_SECURITY_CODE or complete one manual auth bootstrap to persist session state.",
            )
        if is_invalid_credentials(page):
            return False, "Invalid Greenhouse credentials."

    code_selector = first_visible_selector(page, CODE_INPUT_SELECTORS)
    if code_selector:
        if allow_manual_auth:
            return wait_for_manual_verification(page, timeout_ms=manual_auth_timeout_ms)
        return (
            False,
            "Greenhouse login requires a security code. Complete one manual auth bootstrap to persist session state.",
        )

    return False, "Login did not complete. Check credentials/MFA/challenge."


def main() -> int:
    careers_url = (os.getenv("CAREERS_URL", "") or "").strip() or "https://my.greenhouse.io/jobs"
    email = sanitized_secret("MY_GREENHOUSE_EMAIL") or sanitized_secret("GREENHOUSE_EMAIL")
    password = sanitized_secret("MY_GREENHOUSE_PASSWORD") or sanitized_secret("GREENHOUSE_PASSWORD")
    security_code = sanitized_secret("MY_GREENHOUSE_SECURITY_CODE")

    if not email or not password:
        return emit(
            "error",
            "Missing MY_GREENHOUSE_EMAIL/GREENHOUSE_EMAIL or MY_GREENHOUSE_PASSWORD/GREENHOUSE_PASSWORD.",
        )

    timeout_ms = max(10000, int(os.getenv("MY_GREENHOUSE_TIMEOUT_MS", "90000")))
    max_jobs = max(1, int(os.getenv("MY_GREENHOUSE_MAX_JOBS", "300")))
    headless = truthy_env("MY_GREENHOUSE_HEADLESS", True)
    login_headless = truthy_env("MY_GREENHOUSE_LOGIN_HEADLESS", False)
    force_login = truthy_env("MY_GREENHOUSE_FORCE_LOGIN", False)
    manual_auth_timeout_ms = max(15000, int(os.getenv("MY_GREENHOUSE_MANUAL_AUTH_TIMEOUT_MS", "240000")))
    detail_fetch_limit = max(0, int(os.getenv("MY_GREENHOUSE_DETAIL_FETCH_LIMIT", str(max_jobs))))
    wait_after_load_ms = max(0, int(os.getenv("MY_GREENHOUSE_WAIT_AFTER_LOAD_MS", "2500")))
    load_more_pages = max(0, int(os.getenv("MY_GREENHOUSE_LOAD_MORE_PAGES", "60")))
    query = (os.getenv("MY_GREENHOUSE_QUERY", "") or "").strip()
    location_filter = (os.getenv("MY_GREENHOUSE_LOCATION", "") or "").strip()
    configured_storage_state = Path(
        (os.getenv("MY_GREENHOUSE_STORAGE_STATE", "") or "").strip()
        or ".local/my_greenhouse_storage_state.json"
    ).expanduser()
    storage_state_path = prefer_legacy_or_local(
        "my_greenhouse_storage_state.json",
        ".state/my_greenhouse_storage_state.json",
    )
    if configured_storage_state.is_absolute():
        storage_state_path = configured_storage_state
    elif str(configured_storage_state).strip() and configured_storage_state.name != "my_greenhouse_storage_state.json":
        storage_state_path = (Path.cwd() / configured_storage_state).resolve(strict=False)

    allow_manual_auth = truthy_env("MY_GREENHOUSE_ALLOW_MANUAL_AUTH", not login_headless)

    try:
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import sync_playwright
    except Exception:
        return emit(
            "error",
            "playwright is not installed. Install with: uv sync --extra playwright && uv run playwright install chromium",
        )

    storage_state_path.parent.mkdir(parents=True, exist_ok=True)
    try:
        with sync_playwright() as pw:
            def status_from_login_message(login_message: str) -> str:
                lowered = login_message.lower()
                if "challenge" in lowered or "captcha" in lowered:
                    return "blocked"
                return "error"

            def refresh_auth_state() -> tuple[bool, str, str]:
                auth_browser = pw.chromium.launch(headless=login_headless)
                try:
                    auth_context = (
                        auth_browser.new_context(storage_state=str(storage_state_path))
                        if storage_state_path.exists() and not force_login
                        else auth_browser.new_context()
                    )
                    auth_page = auth_context.new_page()
                    response = auth_page.goto(careers_url, wait_until="domcontentloaded", timeout=timeout_ms)
                    message = blocked_message(auth_page, response)
                    if message:
                        return False, "blocked", message

                    if has_login_form(auth_page):
                        success, login_message = perform_login(
                            auth_page,
                            email=email,
                            password=password,
                            security_code=security_code,
                            timeout_ms=timeout_ms,
                            allow_manual_auth=allow_manual_auth,
                            manual_auth_timeout_ms=manual_auth_timeout_ms,
                        )
                        if not success:
                            return False, status_from_login_message(login_message), login_message

                    auth_context.storage_state(path=str(storage_state_path))
                    return True, "ok", "Authentication state refreshed."
                finally:
                    auth_browser.close()

            def open_jobs_page():
                scrape_browser = pw.chromium.launch(headless=headless)
                scrape_context = (
                    scrape_browser.new_context(storage_state=str(storage_state_path))
                    if storage_state_path.exists()
                    else scrape_browser.new_context()
                )
                scrape_page = scrape_context.new_page()
                response = scrape_page.goto(careers_url, wait_until="domcontentloaded", timeout=timeout_ms)
                return scrape_browser, scrape_context, scrape_page, response

            if force_login or not storage_state_path.exists():
                ok, status, message = refresh_auth_state()
                if not ok:
                    return emit(status, message)

            browser, context, page, response = open_jobs_page()
            message = blocked_message(page, response)
            if message:
                browser.close()
                return emit("blocked", message)

            if has_login_form(page):
                browser.close()
                ok, status, message = refresh_auth_state()
                if not ok:
                    return emit(status, message)
                browser, context, page, response = open_jobs_page()
                message = blocked_message(page, response)
                if message:
                    browser.close()
                    return emit("blocked", message)
                if has_login_form(page):
                    browser.close()
                    return emit("error", "Still on login form after refreshed auth state.")

            try:
                page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 20000))
            except PlaywrightTimeoutError:
                pass
            if wait_after_load_ms:
                page.wait_for_timeout(wait_after_load_ms)

            # Scroll a bit to let lazy-loaded listings render.
            for _ in range(8):
                page.mouse.wheel(0, 1800)
                page.wait_for_timeout(250)
            load_more_jobs(
                page,
                timeout_ms=min(timeout_ms, 12000),
                max_pages=load_more_pages,
                max_jobs=max_jobs,
            )

            raw_links = extract_result_cards(page)
            if not raw_links:
                raw_links = extract_fallback_links(page)

            jobs = collect_jobs_from_links(raw_links=raw_links, careers_url=careers_url, max_jobs=max_jobs)
            enrich_jobs(jobs=jobs, timeout_ms=timeout_ms, detail_fetch_limit=detail_fetch_limit)
            filtered_jobs = filter_jobs(jobs=jobs, query=query, location_filter=location_filter)

            context.storage_state(path=str(storage_state_path))
            browser.close()
            return emit("ok", f"Extracted {len(filtered_jobs)} my.greenhouse.io job link(s).", filtered_jobs)
    except Exception as exc:  # noqa: BLE001
        message = f"{type(exc).__name__}: {exc}"
        lowered = message.lower()
        if any(token in lowered for token in ("403", "429", "captcha", "challenge", "access denied")):
            return emit("blocked", message)
        return emit("error", message)


if __name__ == "__main__":
    sys.exit(main())
