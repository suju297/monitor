from __future__ import annotations

import json
import os
import sqlite3
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

from .greenhouse_assistant import detect_greenhouse_application
from .greenhouse_trace_store import DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE
from .utils import utc_now

DEFAULT_JOBS_DB_FILE = ".state/jobs.db"

_ASSISTANT_SYNC_COLUMNS = {
    "assistant_last_sync_at": "TEXT",
    "assistant_last_source": "TEXT NOT NULL DEFAULT ''",
    "assistant_last_outcome": "TEXT NOT NULL DEFAULT ''",
    "assistant_last_trace_id": "TEXT NOT NULL DEFAULT ''",
    "assistant_last_manual_session_id": "TEXT NOT NULL DEFAULT ''",
    "assistant_last_auto_submit_eligible": "INTEGER NOT NULL DEFAULT 0",
    "assistant_last_review_pending_count": "INTEGER NOT NULL DEFAULT 0",
    "assistant_last_confirmation_detected": "INTEGER NOT NULL DEFAULT 0",
}


def _jobs_db_enabled() -> bool:
    raw = str(os.getenv("JOBS_DB_ENABLED", "true")).strip().lower()
    return raw not in {"0", "false", "no", "off"}


def _resolve_jobs_db_path(path: str | Path | None) -> Path:
    configured = str(os.getenv("JOBS_DB_PATH", "")).strip()
    if configured:
        return Path(configured).expanduser()
    if path:
        return Path(path).expanduser()
    return Path(DEFAULT_JOBS_DB_FILE).expanduser()


def _normalized_url(value: str | None) -> str:
    raw = str(value or "").strip()
    if not raw:
        return ""
    parsed = urlparse(raw)
    return parsed._replace(fragment="").geturl().strip().lower()


def _normalized_url_without_query(value: str | None) -> str:
    raw = str(value or "").strip()
    if not raw:
        return ""
    parsed = urlparse(raw)
    return parsed._replace(query="", fragment="").geturl().strip().lower()


def _candidate_match_payloads(*urls: str | None) -> tuple[set[str], set[str], set[tuple[str, str]]]:
    exact_urls: set[str] = set()
    base_urls: set[str] = set()
    job_keys: set[tuple[str, str]] = set()
    for url in urls:
        normalized = _normalized_url(url)
        if normalized:
            exact_urls.add(normalized)
        base = _normalized_url_without_query(url)
        if base:
            base_urls.add(base)
        detection = detect_greenhouse_application(str(url or ""))
        if detection.is_greenhouse and detection.is_application and detection.board_token and detection.job_id:
            job_keys.add((detection.board_token.strip().lower(), detection.job_id.strip().lower()))
    return exact_urls, base_urls, job_keys


def _ensure_jobs_assistant_columns(conn: sqlite3.Connection) -> None:
    rows = conn.execute("PRAGMA table_info(jobs)").fetchall()
    existing = {str(row[1]).strip() for row in rows}
    for column, definition in _ASSISTANT_SYNC_COLUMNS.items():
        if column in existing:
            continue
        conn.execute(f"ALTER TABLE jobs ADD COLUMN {column} {definition}")


def _matching_fingerprints(
    conn: sqlite3.Connection,
    *,
    requested_url: str,
    public_url: str | None,
) -> list[str]:
    exact_urls, base_urls, job_keys = _candidate_match_payloads(requested_url, public_url)
    if not exact_urls and not base_urls and not job_keys:
        return []

    clauses: list[str] = []
    args: list[Any] = []
    for value in sorted(exact_urls):
        clauses.append("lower(url) = ?")
        args.append(value)
    for value in sorted(base_urls):
        clauses.append("lower(url) LIKE ?")
        args.append(value + "%")
    for board_token, job_id in sorted(job_keys):
        clauses.append("lower(url) LIKE ?")
        args.append(f"%/{board_token}/jobs/{job_id}%")
    query = "SELECT fingerprint, url FROM jobs WHERE url IS NOT NULL AND (" + " OR ".join(clauses) + ")"

    matches: list[str] = []
    seen: set[str] = set()
    for row in conn.execute(query, args):
        fingerprint = str(row[0] or "").strip()
        row_url = str(row[1] or "").strip()
        if not fingerprint or not row_url:
            continue
        normalized = _normalized_url(row_url)
        base = _normalized_url_without_query(row_url)
        matched = normalized in exact_urls or base in base_urls
        if not matched and job_keys:
            detection = detect_greenhouse_application(row_url)
            if detection.is_greenhouse and detection.is_application and detection.board_token and detection.job_id:
                matched = (
                    detection.board_token.strip().lower(),
                    detection.job_id.strip().lower(),
                ) in job_keys
        if not matched or fingerprint in seen:
            continue
        seen.add(fingerprint)
        matches.append(fingerprint)
    return matches


def sync_greenhouse_job_record(
    *,
    requested_url: str,
    public_url: str | None = None,
    source: str,
    outcome: str,
    application_status: str | None = None,
    trace_id: str | None = None,
    manual_session_id: str | None = None,
    auto_submit_eligible: bool = False,
    review_pending_count: int = 0,
    confirmation_detected: bool = False,
    jobs_db_file: str | Path = DEFAULT_JOBS_DB_FILE,
    enabled: bool = True,
) -> dict[str, Any]:
    if not enabled:
        return {"stored": False, "reason": "disabled", "matched_count": 0, "fingerprints": []}
    if not _jobs_db_enabled():
        return {"stored": False, "reason": "jobs_db_disabled", "matched_count": 0, "fingerprints": []}

    db_path = _resolve_jobs_db_path(jobs_db_file)
    if not db_path.is_absolute():
        db_path = Path.cwd() / db_path
    if not db_path.exists():
        return {
            "stored": False,
            "reason": "jobs_db_missing",
            "db_path": str(db_path),
            "matched_count": 0,
            "fingerprints": [],
        }

    sync_at = utc_now()
    conn = sqlite3.connect(str(db_path), timeout=10)
    try:
        conn.execute("PRAGMA busy_timeout = 10000")
        _ensure_jobs_assistant_columns(conn)
        fingerprints = _matching_fingerprints(
            conn,
            requested_url=requested_url,
            public_url=public_url,
        )
        if not fingerprints:
            conn.commit()
            return {
                "stored": False,
                "reason": "job_not_found",
                "db_path": str(db_path),
                "matched_count": 0,
                "fingerprints": [],
            }

        normalized_status = str(application_status or "").strip().lower()
        if normalized_status not in {"", "applied", "not_applied"}:
            normalized_status = ""
        application_updated_at = sync_at if normalized_status else None
        for fingerprint in fingerprints:
            conn.execute(
                """
                UPDATE jobs
                SET
                    application_status = CASE WHEN ? <> '' THEN ? ELSE application_status END,
                    application_updated_at = CASE WHEN ? IS NOT NULL THEN ? ELSE application_updated_at END,
                    assistant_last_sync_at = ?,
                    assistant_last_source = ?,
                    assistant_last_outcome = ?,
                    assistant_last_trace_id = ?,
                    assistant_last_manual_session_id = ?,
                    assistant_last_auto_submit_eligible = ?,
                    assistant_last_review_pending_count = ?,
                    assistant_last_confirmation_detected = ?
                WHERE fingerprint = ?
                """,
                (
                    normalized_status,
                    normalized_status,
                    application_updated_at,
                    application_updated_at,
                    sync_at,
                    str(source or "").strip(),
                    str(outcome or "").strip(),
                    str(trace_id or "").strip(),
                    str(manual_session_id or "").strip(),
                    1 if auto_submit_eligible else 0,
                    max(0, int(review_pending_count or 0)),
                    1 if confirmation_detected else 0,
                    fingerprint,
                ),
            )
        conn.commit()
        return {
            "stored": True,
            "db_path": str(db_path),
            "matched_count": len(fingerprints),
            "fingerprints": fingerprints,
            "application_status": normalized_status,
            "assistant_last_sync_at": sync_at,
        }
    finally:
        conn.close()


def backfill_jobs_db_from_manual_sessions(
    *,
    session_file: str | Path = DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
    jobs_db_file: str | Path = DEFAULT_JOBS_DB_FILE,
    enabled: bool = True,
) -> dict[str, Any]:
    if not enabled:
        return {"stored": False, "reason": "disabled", "attempted": 0, "stored_count": 0, "matched_count": 0}
    if not _jobs_db_enabled():
        return {
            "stored": False,
            "reason": "jobs_db_disabled",
            "attempted": 0,
            "stored_count": 0,
            "matched_count": 0,
        }

    session_path = Path(session_file).expanduser()
    if not session_path.is_absolute():
        session_path = Path.cwd() / session_path
    if not session_path.exists():
        return {
            "stored": False,
            "reason": "session_file_missing",
            "session_file": str(session_path),
            "attempted": 0,
            "stored_count": 0,
            "matched_count": 0,
        }

    attempted = 0
    stored_count = 0
    matched_count = 0
    bad_records = 0
    session_ids: list[str] = []
    sync_results: list[dict[str, Any]] = []
    seen_session_ids: set[str] = set()

    with session_path.open("r", encoding="utf-8") as handle:
        for raw_line in handle:
            line = raw_line.strip()
            if not line:
                continue
            try:
                record = json.loads(line)
            except json.JSONDecodeError:
                bad_records += 1
                continue

            session_id = str(record.get("manual_session_id") or "").strip()
            if session_id and session_id in seen_session_ids:
                continue
            if session_id:
                seen_session_ids.add(session_id)
                session_ids.append(session_id)

            requested_url = (
                str(record.get("requested_url") or "").strip()
                or str(record.get("public_url") or "").strip()
                or str(record.get("final_page_url") or "").strip()
            )
            if not requested_url:
                bad_records += 1
                continue

            attempted += 1
            submitted = bool(record.get("submitted")) or bool(record.get("confirmation_detected"))
            result = sync_greenhouse_job_record(
                requested_url=requested_url,
                public_url=str(record.get("public_url") or "").strip() or None,
                source="manual_observer_backfill",
                outcome="submitted" if submitted else "observed",
                application_status="applied" if submitted else None,
                manual_session_id=session_id or None,
                auto_submit_eligible=False,
                review_pending_count=0,
                confirmation_detected=bool(record.get("confirmation_detected")),
                jobs_db_file=jobs_db_file,
                enabled=True,
            )
            sync_results.append(
                {
                    "manual_session_id": session_id or None,
                    "requested_url": requested_url,
                    "stored": bool(result.get("stored")),
                    "matched_count": int(result.get("matched_count") or 0),
                    "reason": str(result.get("reason") or "").strip() or None,
                }
            )
            if result.get("stored"):
                stored_count += 1
                matched_count += int(result.get("matched_count") or 0)

    return {
        "stored": stored_count > 0,
        "reason": "" if stored_count > 0 else "no_matching_jobs_updated",
        "session_file": str(session_path),
        "attempted": attempted,
        "stored_count": stored_count,
        "matched_count": matched_count,
        "bad_records": bad_records,
        "manual_session_ids": session_ids,
        "results": sync_results,
    }
