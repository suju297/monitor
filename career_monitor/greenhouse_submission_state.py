from __future__ import annotations

import json
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit, urlunsplit

from .utils import utc_now

DEFAULT_GREENHOUSE_SUBMISSION_STATE_FILE = ".state/greenhouse_submissions.json"


def load_greenhouse_submission_state(state_path: Path) -> dict[str, Any]:
    if not state_path.exists():
        return {"applications": {}}
    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        state = {}
    if not isinstance(state, dict):
        state = {}
    state.setdefault("applications", {})
    return state


def save_greenhouse_submission_state(state_path: Path, state: dict[str, Any]) -> None:
    state_path.parent.mkdir(parents=True, exist_ok=True)
    state_path.write_text(json.dumps(state, indent=2, sort_keys=True), encoding="utf-8")


def normalize_submission_url(url: str | None) -> str:
    raw = (url or "").strip()
    if not raw:
        return ""
    parsed = urlsplit(raw)
    scheme = (parsed.scheme or "https").lower()
    netloc = parsed.netloc.lower()
    path = parsed.path.rstrip("/")
    return urlunsplit((scheme, netloc, path, parsed.query, ""))


def build_greenhouse_application_key(
    *,
    board_token: str | None,
    job_id: str | None,
    public_url: str | None,
) -> str:
    if board_token and job_id:
        return f"greenhouse:{board_token}:{job_id}"
    normalized_url = normalize_submission_url(public_url)
    if normalized_url:
        return f"url:{normalized_url}"
    raise ValueError("Either board_token/job_id or public_url is required to build an application key.")


def get_submission_record(
    state: dict[str, Any],
    *,
    application_key: str,
) -> dict[str, Any] | None:
    applications = state.get("applications")
    if not isinstance(applications, dict):
        return None
    record = applications.get(application_key)
    if isinstance(record, dict):
        return record
    return None


def record_submission(
    state: dict[str, Any],
    *,
    application_key: str,
    board_token: str | None,
    job_id: str | None,
    company_name: str | None,
    title: str | None,
    public_url: str,
    confirmation_url: str,
    confirmation_note: str | None,
    page_url: str,
) -> dict[str, Any]:
    applications = state.setdefault("applications", {})
    record = {
        "application_key": application_key,
        "board_token": board_token,
        "job_id": job_id,
        "company_name": company_name,
        "title": title,
        "public_url": normalize_submission_url(public_url),
        "page_url": normalize_submission_url(page_url),
        "confirmation_url": normalize_submission_url(confirmation_url),
        "confirmation_note": (confirmation_note or "").strip() or None,
        "submitted_at": utc_now(),
    }
    applications[application_key] = record
    return record
