#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import sys
import xml.etree.ElementTree as ET
from typing import Any
from urllib.parse import parse_qsl, urlencode, urlparse, urlunparse

import requests
from bs4 import BeautifulSoup

DEFAULT_CAREERS_URL = "https://bloomberg.avature.net/careers/SearchJobs"
DEFAULT_QUERY = "software"

USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) "
    "Chrome/124.0.0.0 Safari/537.36"
)

BLOCKED_TITLE_MARKERS = (
    "attention required",
    "just a moment",
    "are you a robot",
    "security checkpoint",
    "captcha",
    "access denied",
)

US_STATE_NAME_RE = re.compile(
    r"(?i)\\b(?:alabama|alaska|arizona|arkansas|california|colorado|connecticut|delaware|florida|"
    r"georgia|hawaii|idaho|illinois|indiana|iowa|kansas|kentucky|louisiana|maine|maryland|"
    r"massachusetts|michigan|minnesota|mississippi|missouri|montana|nebraska|nevada|new hampshire|"
    r"new jersey|new mexico|new york|north carolina|north dakota|ohio|oklahoma|oregon|pennsylvania|"
    r"rhode island|south carolina|south dakota|tennessee|texas|utah|vermont|virginia|washington|"
    r"west virginia|wisconsin|wyoming|district of columbia|washington d\\.?c\\.?)\\b"
)
US_STATE_CODE_RE = re.compile(
    r"(?i)(?:,\\s*|-\\s*|\\b)(?:AL|AK|AZ|AR|CA|CO|CT|DE|FL|GA|HI|IA|ID|IL|IN|KS|KY|LA|MA|MD|ME|"
    r"MI|MN|MO|MS|MT|NC|ND|NE|NH|NJ|NM|NV|NY|OH|OK|OR|PA|RI|SC|SD|TN|TX|UT|VA|VT|WA|WI|WV|WY|DC)\\b"
)
US_COUNTRY_RE = re.compile(r"(?i)\\b(?:united states(?: of america)?|u\\.?s\\.?a?\\.?|usa)\\b")

US_CITY_HINTS = (
    "new york",
    "nyc",
    "san francisco",
    "palo alto",
    "princeton",
    "arlington",
    "washington",
    "boston",
    "seattle",
    "austin",
    "chicago",
    "atlanta",
    "dallas",
    "houston",
    "denver",
    "los angeles",
)

NON_US_LOCATION_MARKERS = (
    "london",
    "tokyo",
    "singapore",
    "hong kong",
    "toronto",
    "mumbai",
    "sao paulo",
    "são paulo",
    "melbourne",
    "sydney",
    "frankfurt",
    "paris",
    "dublin",
    "amsterdam",
    "zurich",
    "geneva",
    "seoul",
    "beijing",
)


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


def normalize_text(value: Any, limit: int = 320) -> str:
    text = re.sub(r"\\s+", " ", str(value or "")).strip()
    if not text:
        return ""
    return f"{text[:limit].rstrip()}..." if len(text) > limit else text


def parse_query_terms(raw: str) -> list[str]:
    text = raw.strip().lower()
    if not text:
        return []
    if "," in text:
        parts = [item.strip() for item in text.split(",")]
    else:
        parts = re.split(r"\\s+", text)
    return [term for term in parts if term]


def is_blocked_text(value: str) -> bool:
    text = (value or "").lower()
    return any(marker in text for marker in BLOCKED_TITLE_MARKERS)


def derive_feed_base_url(careers_url: str) -> str:
    raw = careers_url.strip() or DEFAULT_CAREERS_URL
    parsed = urlparse(raw)
    if not parsed.scheme:
        parsed = urlparse(f"https://{raw}")
    path = parsed.path
    if "/feed" not in path.lower():
        path = path.rstrip("/") + "/feed/"
    elif not path.endswith("/"):
        path += "/"
    return urlunparse((parsed.scheme, parsed.netloc, path, "", parsed.query, ""))


def build_feed_page_url(feed_base_url: str, page_size: int, offset: int) -> str:
    parsed = urlparse(feed_base_url)
    query_pairs = dict(parse_qsl(parsed.query, keep_blank_values=True))
    query_pairs["jobRecordsPerPage"] = str(page_size)
    if offset > 0:
        query_pairs["jobOffset"] = str(offset)
    else:
        query_pairs.pop("jobOffset", None)
    query = urlencode(query_pairs, doseq=True, safe="[]")
    return urlunparse((parsed.scheme, parsed.netloc, parsed.path, "", query, ""))


def parse_feed_items(feed_xml: str) -> list[dict[str, str]]:
    try:
        root = ET.fromstring(feed_xml)
    except ET.ParseError:
        return []

    out: list[dict[str, str]] = []
    for item in root.findall("./channel/item"):
        title = normalize_text(item.findtext("title", ""), 280)
        link = normalize_text(item.findtext("link", ""), 900)
        pub_date = normalize_text(item.findtext("pubDate", ""), 120)
        description = normalize_text(item.findtext("description", ""), 400)
        out.append(
            {
                "title": title,
                "link": link,
                "posted_at": pub_date,
                "description": description,
            }
        )
    return out


def looks_like_job_detail_url(url: str) -> bool:
    normalized = url.strip().lower()
    return "/careers/jobdetail/" in normalized


def infer_us_location(location: str, title: str) -> str:
    value = normalize_text(location, 180)
    if not value:
        title_lower = title.lower()
        if "(ny" in title_lower or " ny)" in title_lower or "nyc" in title_lower:
            return "New York, United States"
        return ""

    lower = value.lower()
    if US_COUNTRY_RE.search(lower) or US_STATE_NAME_RE.search(lower) or US_STATE_CODE_RE.search(lower):
        return value
    for city in US_CITY_HINTS:
        if city in lower:
            return normalize_text(f"{value}, United States", 220)
    return value


def is_us_job(location: str, title: str, url: str, description: str) -> bool:
    joined = " ".join([location, title, url, description]).lower()
    if US_COUNTRY_RE.search(joined):
        return True
    if US_STATE_NAME_RE.search(joined) or US_STATE_CODE_RE.search(joined):
        return True

    for marker in NON_US_LOCATION_MARKERS:
        if marker in joined:
            return False

    # Accept known US-city-only locations as US-based even when state/country is omitted.
    for city in US_CITY_HINTS:
        if city in joined:
            return True
    return False


def extract_external_id(url: str, fallback_text: str = "") -> str:
    url_match = re.search(r"/JobDetail/[^/]+/(\\d+)", url)
    if url_match:
        return url_match.group(1)
    ref_match = re.search(r"(\\d{5,})", fallback_text)
    if ref_match:
        return ref_match.group(1)
    return ""


def matches_query(title: str, description: str, url: str, terms: list[str]) -> bool:
    if not terms:
        return True
    haystack = " ".join([title, description, url]).lower()
    if not haystack:
        return False
    return any(term in haystack for term in terms)


def extract_detail_payload(html_doc: str) -> dict[str, str]:
    soup = BeautifulSoup(html_doc, "html.parser")

    title = ""
    og_title = soup.select_one('meta[property="og:title"]')
    if og_title:
        title = normalize_text(og_title.get("content", ""), 280)
    if not title:
        node = soup.select_one(".article__content__view__field__value--font .article__content__view__field__value")
        if node:
            title = normalize_text(node.get_text(" ", strip=True), 280)

    fields: dict[str, str] = {}
    for row in soup.select("div.article__content__view__field"):
        label_node = row.select_one(".article__content__view__field__label")
        value_node = row.select_one(".article__content__view__field__value")
        if not label_node or not value_node:
            continue
        label = normalize_text(label_node.get_text(" ", strip=True), 80).lower()
        value = normalize_text(value_node.get_text(" ", strip=True), 1800)
        if label and value and label not in fields:
            fields[label] = value

    description = ""
    for article in soup.select("article.article"):
        header = article.select_one(".article__header__text__title")
        header_text = normalize_text(header.get_text(" ", strip=True), 160).lower() if header else ""
        if "description" not in header_text:
            continue
        value_node = article.select_one(".field--rich-text .article__content__view__field__value")
        if value_node:
            description = normalize_text(value_node.get_text(" ", strip=True), 5000)
            if len(description) >= 80:
                break

    if not description:
        candidates = []
        for node in soup.select(".field--rich-text .article__content__view__field__value"):
            text = normalize_text(node.get_text(" ", strip=True), 5000)
            if len(text) >= 120:
                candidates.append(text)
        if candidates:
            candidates.sort(key=len, reverse=True)
            description = candidates[0]

    return {
        "title": title,
        "location": fields.get("location", ""),
        "team": fields.get("business area", ""),
        "external_id": fields.get("ref #", ""),
        "description": description,
    }


def fetch_detail(
    session: requests.Session,
    url: str,
    timeout_seconds: int,
) -> tuple[str, str, dict[str, str]]:
    try:
        response = session.get(
            url,
            timeout=timeout_seconds,
            headers={"User-Agent": USER_AGENT, "Accept": "text/html,application/xhtml+xml"},
        )
    except requests.RequestException as exc:
        return "error", f"RequestException: {exc}", {}

    if response.status_code in {401, 403, 429}:
        return "blocked", f"HTTP {response.status_code} on detail page", {}
    if response.status_code >= 400:
        return "error", f"HTTP {response.status_code} on detail page", {}

    html_doc = response.text or ""
    if is_blocked_text(response.url) or is_blocked_text(html_doc[:1200]):
        return "blocked", "Challenge page detected while fetching detail", {}

    payload = extract_detail_payload(html_doc)
    return "ok", "", payload


def main() -> int:
    careers_url = (os.getenv("CAREERS_URL") or "").strip() or DEFAULT_CAREERS_URL
    feed_base_url = derive_feed_base_url(careers_url)

    timeout_seconds = parse_int_env("BLOOMBERG_TIMEOUT_SECONDS", 35, minimum=5)
    page_size = parse_int_env("BLOOMBERG_PAGE_SIZE", 20, minimum=1, maximum=100)
    max_pages = parse_int_env("BLOOMBERG_MAX_PAGES", 8, minimum=1, maximum=50)
    max_jobs = parse_int_env("BLOOMBERG_MAX_JOBS", 240, minimum=1, maximum=2000)
    detail_fetch_limit = parse_int_env("BLOOMBERG_DETAIL_FETCH_LIMIT", max_jobs, minimum=1)

    us_only = parse_bool_env("BLOOMBERG_US_ONLY", True)
    query_text = (os.getenv("BLOOMBERG_QUERY") or "").strip() or DEFAULT_QUERY
    query_terms = parse_query_terms(query_text)

    session = requests.Session()

    jobs: list[dict[str, Any]] = []
    seen_urls: set[str] = set()

    raw_items_total = 0
    detail_fetches = 0
    blocked_detail_count = 0
    detail_errors = 0

    first_page_blocked_message = ""

    for page_index in range(max_pages):
        offset = page_index * page_size
        feed_page_url = build_feed_page_url(feed_base_url, page_size=page_size, offset=offset)

        try:
            response = session.get(
                feed_page_url,
                timeout=timeout_seconds,
                headers={
                    "User-Agent": USER_AGENT,
                    "Accept": "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8",
                },
            )
        except requests.RequestException as exc:
            if page_index == 0:
                return emit("error", f"RequestException while fetching Bloomberg feed: {exc}")
            break

        if response.status_code in {401, 403, 429}:
            message = f"HTTP {response.status_code} from Bloomberg Avature feed"
            if page_index == 0:
                return emit("blocked", message)
            break
        if response.status_code >= 400:
            message = f"Bloomberg Avature feed HTTP {response.status_code}"
            if page_index == 0:
                return emit("error", message)
            break

        feed_xml = response.text or ""
        if "<rss" not in feed_xml.lower():
            if page_index == 0 and is_blocked_text(feed_xml[:1200]):
                first_page_blocked_message = "Bloomberg feed returned a challenge page"
                break
            if page_index == 0:
                return emit("error", "Bloomberg feed did not return RSS/XML")
            break

        items = parse_feed_items(feed_xml)
        if not items:
            if page_index == 0:
                return emit("error", "Bloomberg feed returned no parseable job items")
            break

        raw_items_total += len(items)
        new_items_this_page = 0

        for item in items:
            title = normalize_text(item.get("title", ""), 280)
            job_url = normalize_text(item.get("link", ""), 900)
            posted_at = normalize_text(item.get("posted_at", ""), 120)
            item_desc = normalize_text(item.get("description", ""), 400)

            if not title or not job_url:
                continue
            if not looks_like_job_detail_url(job_url):
                continue
            if job_url in seen_urls:
                continue

            seen_urls.add(job_url)
            new_items_this_page += 1

            detail_payload: dict[str, str] = {}
            if detail_fetches < detail_fetch_limit:
                detail_fetches += 1
                detail_status, _, detail_payload = fetch_detail(
                    session=session,
                    url=job_url,
                    timeout_seconds=timeout_seconds,
                )
                if detail_status == "blocked":
                    blocked_detail_count += 1
                elif detail_status == "error":
                    detail_errors += 1

            final_title = normalize_text(detail_payload.get("title") or title, 280)
            location = infer_us_location(detail_payload.get("location", ""), final_title)
            team = normalize_text(detail_payload.get("team", ""), 220)
            description = normalize_text(detail_payload.get("description", ""), 5000)

            if not description:
                description = item_desc

            if query_terms and not matches_query(final_title, description, job_url, query_terms):
                continue

            if us_only and not is_us_job(location=location, title=final_title, url=job_url, description=description):
                continue

            external_id = extract_external_id(job_url, detail_payload.get("external_id", ""))

            jobs.append(
                {
                    "external_id": external_id or None,
                    "title": final_title,
                    "url": job_url,
                    "location": location or None,
                    "team": team or None,
                    "posted_at": posted_at or None,
                    "description": description or None,
                }
            )

            if len(jobs) >= max_jobs:
                break

        if len(jobs) >= max_jobs:
            break
        if new_items_this_page == 0 or len(items) < page_size:
            break

    if first_page_blocked_message:
        return emit("blocked", first_page_blocked_message)

    if not jobs and blocked_detail_count > 0 and blocked_detail_count >= detail_fetches > 0:
        return emit("blocked", "Bloomberg detail pages were blocked while extracting jobs")

    message = (
        f"Extracted {len(jobs)} Bloomberg Avature job(s) "
        f"({raw_items_total} feed item(s), {detail_fetches} detail fetch(es), {detail_errors} detail error(s))"
    )
    return emit("ok", message, jobs)


if __name__ == "__main__":
    sys.exit(main())
