from __future__ import annotations

import os
import re
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import urljoin, urlparse

from .constants import BLOCKED_TITLE_MARKERS, JOB_KEYWORDS

US_STATE_CODES = (
    "AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS", "KY",
    "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV", "NY",
    "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY", "DC",
)
US_COUNTRY_RE = re.compile(r"\b(?:united states(?: of america)?|u\.s\.a?\.|usa)\b", flags=re.IGNORECASE)
US_REMOTE_RE = re.compile(
    r"\bremote\b(?:[^a-z0-9]{0,12})(?:u\.s\.a?\.|usa|united states|us)\b|\b(?:u\.s\.a?\.|usa|united states|us)\b(?:[^a-z0-9]{0,12})\bremote\b",
    flags=re.IGNORECASE,
)
US_STATE_NAME_RE = re.compile(
    r"\b(?:alabama|alaska|arizona|arkansas|california|colorado|connecticut|delaware|florida|georgia|hawaii|idaho|illinois|indiana|iowa|kansas|kentucky|louisiana|maine|maryland|massachusetts|michigan|minnesota|mississippi|missouri|montana|nebraska|nevada|new hampshire|new jersey|new mexico|new york|north carolina|north dakota|ohio|oklahoma|oregon|pennsylvania|rhode island|south carolina|south dakota|tennessee|texas|utah|vermont|virginia|washington|west virginia|wisconsin|wyoming|district of columbia|washington d\.?c\.?)\b",
    flags=re.IGNORECASE,
)
US_STATE_CODE_RE = re.compile(r"(?:,\s*|-\s*|\b)(?:" + "|".join(US_STATE_CODES) + r")\b", flags=re.IGNORECASE)
US_ONLY_REGEX_ENV = ("US_ONLY_REGEX", "US_ONLY_JOBS_REGEX")
_US_CUSTOM_RE: re.Pattern[str] | None = None
_US_CUSTOM_RE_LOADED = False


def utc_now() -> str:
    return datetime.now(tz=timezone.utc).isoformat(timespec="seconds")


def parse_bool_env(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


def load_dotenv(dotenv_path: Path) -> None:
    if not dotenv_path.exists():
        return
    for raw_line in dotenv_path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip().strip('"').strip("'"))


def normalize_created_at(value: Any) -> str | None:
    if value is None:
        return None
    if isinstance(value, int):
        return datetime.fromtimestamp(value / 1000, tz=timezone.utc).isoformat(timespec="seconds")
    return str(value)


def parse_greenhouse_board(careers_url: str) -> str | None:
    parsed = urlparse(careers_url)
    if "greenhouse.io" not in parsed.netloc:
        return None
    parts = [part for part in parsed.path.split("/") if part]
    return parts[-1] if parts else None


def parse_lever_site(careers_url: str) -> str | None:
    parsed = urlparse(careers_url)
    if "lever.co" not in parsed.netloc:
        return None
    parts = [part for part in parsed.path.split("/") if part]
    return parts[-1] if parts else None


def parse_icims_api_endpoint(careers_url: str) -> str | None:
    parsed = urlparse(careers_url)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        return None
    return f"{parsed.scheme}://{parsed.netloc}/api/jobs"


def parse_icims_jobs_base_path(careers_url: str) -> str:
    parsed = urlparse(careers_url)
    parts = [part for part in parsed.path.split("/") if part]
    for idx, part in enumerate(parts):
        if part.lower() == "jobs":
            return "/" + "/".join(parts[: idx + 1])
    return "/jobs"


def build_icims_job_url(
    careers_url: str,
    req_id: str,
    language: str | None,
    canonical_url: str | None = None,
) -> str:
    if canonical_url:
        absolute = urljoin(careers_url, canonical_url)
        parsed = urlparse(absolute)
        if parsed.scheme in {"http", "https"} and parsed.netloc:
            return absolute

    parsed = urlparse(careers_url)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        return careers_url
    detail_base = parse_icims_jobs_base_path(careers_url)
    base = f"{parsed.scheme}://{parsed.netloc}{detail_base}"
    req_segment = req_id.strip() or ""
    if req_segment:
        base = f"{base}/{req_segment}"
    language_segment = (language or "").strip()
    if language_segment:
        return f"{base}?lang={language_segment}"
    return base


def looks_like_job_link(text: str, url: str) -> bool:
    combined = f"{text} {url}".lower()
    return any(keyword in combined for keyword in JOB_KEYWORDS)


def title_from_url(url: str) -> str:
    parsed = urlparse(url)
    path_parts = [part for part in parsed.path.split("/") if part]
    if not path_parts:
        return "Possible opening"
    raw = path_parts[-1]
    cleaned = re.sub(r"[-_]+", " ", raw).strip()
    cleaned = re.sub(r"\s+", " ", cleaned)
    return cleaned[:150] or "Possible opening"


def is_challenge_title(title: str) -> bool:
    normalized = title.strip().lower()
    if not normalized:
        return False
    return any(marker in normalized for marker in BLOCKED_TITLE_MARKERS)


def classify_http_block(status_code: int, title: str) -> bool:
    if status_code in {401, 403, 429}:
        return True
    if is_challenge_title(title):
        return True
    return False


def custom_us_regex() -> re.Pattern[str] | None:
    global _US_CUSTOM_RE_LOADED, _US_CUSTOM_RE
    if _US_CUSTOM_RE_LOADED:
        return _US_CUSTOM_RE
    _US_CUSTOM_RE_LOADED = True
    pattern = ""
    for env_name in US_ONLY_REGEX_ENV:
        value = os.getenv(env_name, "").strip()
        if value:
            pattern = value
            break
    if not pattern:
        return None
    try:
        _US_CUSTOM_RE = re.compile(pattern)
    except re.error:
        _US_CUSTOM_RE = None
    return _US_CUSTOM_RE


def has_us_marker(value: str | None) -> bool:
    raw = (value or "").strip()
    text = raw.lower()
    if not raw:
        return False
    custom_re = custom_us_regex()
    if custom_re and custom_re.search(raw):
        return True
    if US_COUNTRY_RE.search(text):
        return True
    if US_REMOTE_RE.search(text):
        return True
    if US_STATE_NAME_RE.search(text):
        return True
    if US_STATE_CODE_RE.search(text):
        return True
    if "/en-us/" in text or "/us/" in text:
        return True
    if "location=united%20states" in text or "united-states" in text:
        return True
    return False


def is_us_based_job(title: str, location: str | None, url: str) -> bool:
    return has_us_marker(location) or has_us_marker(title) or has_us_marker(url)


def get_nested(value: Any, path: str, default: Any = None) -> Any:
    if not path:
        return value
    current = value
    for part in path.split("."):
        if isinstance(current, dict):
            if part not in current:
                return default
            current = current[part]
            continue
        if isinstance(current, list) and part.isdigit():
            index = int(part)
            if index < 0 or index >= len(current):
                return default
            current = current[index]
            continue
        return default
    return current
