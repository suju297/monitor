from __future__ import annotations

import copy
import json
import secrets
import sqlite3
import threading
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

from .exceptions import CrawlFetchError
from .greenhouse_assistant import detect_greenhouse_application
from .greenhouse_submission_state import build_greenhouse_application_key
from .jobs_db_sync import DEFAULT_JOBS_DB_FILE
from .utils import utc_now

DEFAULT_GREENHOUSE_ASSISTANT_QUEUE_FILE = ".state/greenhouse_assistant_queue.json"
DEFAULT_JOBS_BATCH_LIMIT = 25
MAX_JOBS_BATCH_LIMIT = 100
ACTIVE_QUEUE_STATUSES = {"queued", "running", "review", "ready"}
TERMINAL_QUEUE_STATUSES = {"completed", "failed", "skipped"}


def _resolve_path(path: str | Path) -> Path:
    resolved = Path(path).expanduser()
    if resolved.is_absolute():
        return resolved
    return Path.cwd() / resolved


def _normalized_url(value: str | None, *, strip_query: bool) -> str:
    raw = str(value or "").strip()
    if not raw:
        return ""
    parsed = urlparse(raw)
    if strip_query:
        parsed = parsed._replace(query="", fragment="")
    else:
        parsed = parsed._replace(fragment="")
    return parsed.geturl().strip().lower()


def _queue_identity(url: str) -> str:
    detection = detect_greenhouse_application(url)
    if detection.is_greenhouse and detection.is_application and detection.board_token and detection.job_id:
        return build_greenhouse_application_key(
            board_token=detection.board_token,
            job_id=detection.job_id,
            public_url=url,
        )
    return _normalized_url(url, strip_query=True)


def _new_queue_state() -> dict[str, Any]:
    return {
        "version": 1,
        "updated_at": None,
        "items": [],
    }


def _load_queue_state(path: str | Path) -> dict[str, Any]:
    queue_path = _resolve_path(path)
    if not queue_path.exists():
        return _new_queue_state()
    payload = json.loads(queue_path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        return _new_queue_state()
    items = payload.get("items")
    if not isinstance(items, list):
        payload["items"] = []
    payload.setdefault("version", 1)
    payload.setdefault("updated_at", None)
    return payload


def _save_queue_state(path: str | Path, payload: dict[str, Any]) -> None:
    queue_path = _resolve_path(path)
    queue_path.parent.mkdir(parents=True, exist_ok=True)
    next_payload = copy.deepcopy(payload)
    next_payload["updated_at"] = utc_now()
    temp_path = queue_path.with_suffix(queue_path.suffix + ".tmp")
    temp_path.write_text(json.dumps(next_payload, indent=2, sort_keys=True), encoding="utf-8")
    temp_path.replace(queue_path)


def _queue_status_counts(items: list[dict[str, Any]]) -> dict[str, int]:
    counts: dict[str, int] = {}
    for item in items:
        status = str(item.get("status") or "queued").strip().lower() or "queued"
        counts[status] = counts.get(status, 0) + 1
    return counts


def _queue_item_payload(item: dict[str, Any]) -> dict[str, Any]:
    return {
        "queue_id": str(item.get("queue_id") or "").strip(),
        "status": str(item.get("status") or "queued").strip() or "queued",
        "requested_url": str(item.get("requested_url") or "").strip(),
        "application_key": str(item.get("application_key") or "").strip() or None,
        "company_name": str(item.get("company_name") or "").strip() or None,
        "job_title": str(item.get("job_title") or "").strip() or None,
        "fingerprint": str(item.get("fingerprint") or "").strip() or None,
        "first_seen": str(item.get("first_seen") or "").strip() or None,
        "source": str(item.get("source") or "").strip() or None,
        "application_status": str(item.get("application_status") or "").strip() or None,
        "work_auth_status": str(item.get("work_auth_status") or "").strip() or None,
        "assistant_last_outcome": str(item.get("assistant_last_outcome") or "").strip() or None,
        "assistant_last_source": str(item.get("assistant_last_source") or "").strip() or None,
        "assistant_last_review_pending_count": int(item.get("assistant_last_review_pending_count") or 0),
        "added_by": str(item.get("added_by") or "").strip() or None,
        "added_at": str(item.get("added_at") or "").strip() or None,
        "updated_at": str(item.get("updated_at") or "").strip() or None,
        "run_id": str(item.get("run_id") or "").strip() or None,
        "last_trace_id": str(item.get("last_trace_id") or "").strip() or None,
        "last_error": str(item.get("last_error") or "").strip() or None,
        "review_pending_count": int(item.get("review_pending_count") or 0),
        "submitted": bool(item.get("submitted")),
        "confirmation_detected": bool(item.get("confirmation_detected")),
        "skip_reason": str(item.get("skip_reason") or "").strip() or None,
        "selected_resume_variant": str(item.get("selected_resume_variant") or "").strip() or None,
        "selected_resume_path": str(item.get("selected_resume_path") or "").strip() or None,
        "resume_selection_source": str(item.get("resume_selection_source") or "").strip() or None,
        "resume_selection_updated_at": str(item.get("resume_selection_updated_at") or "").strip() or None,
    }


def _supported_greenhouse_candidate(url: str) -> bool:
    detection = detect_greenhouse_application(url)
    return bool(
        detection.is_greenhouse
        and detection.is_application
        and detection.board_token
        and detection.job_id
    )


def _load_jobs_rows(jobs_db_file: str | Path) -> list[dict[str, Any]]:
    db_path = _resolve_path(jobs_db_file)
    if not db_path.exists():
        raise CrawlFetchError(f"Jobs DB not found: {db_path}")
    conn = sqlite3.connect(str(db_path), timeout=10)
    try:
        conn.execute("PRAGMA busy_timeout = 10000")
        rows = conn.execute(
            """
            SELECT
                fingerprint,
                company,
                title,
                url,
                source,
                first_seen,
                posted_at,
                application_status,
                work_auth_status,
                assistant_last_outcome,
                assistant_last_source,
                assistant_last_review_pending_count,
                assistant_last_confirmation_detected
            FROM jobs
            WHERE active = 1 AND COALESCE(url, '') <> ''
            ORDER BY
                datetime(COALESCE(first_seen, posted_at, '1970-01-01T00:00:00Z')) DESC,
                COALESCE(first_seen, posted_at, '') DESC,
                company ASC,
                title ASC
            """
        ).fetchall()
    finally:
        conn.close()

    results: list[dict[str, Any]] = []
    for row in rows:
        url = str(row[3] or "").strip()
        if not _supported_greenhouse_candidate(url):
            continue
        results.append(
            {
                "fingerprint": str(row[0] or "").strip() or None,
                "company_name": str(row[1] or "").strip() or None,
                "job_title": str(row[2] or "").strip() or None,
                "requested_url": url,
                "source": str(row[4] or "").strip() or None,
                "first_seen": str(row[5] or "").strip() or None,
                "posted_at": str(row[6] or "").strip() or None,
                "application_status": str(row[7] or "").strip() or None,
                "work_auth_status": str(row[8] or "").strip() or None,
                "assistant_last_outcome": str(row[9] or "").strip() or None,
                "assistant_last_source": str(row[10] or "").strip() or None,
                "assistant_last_review_pending_count": int(row[11] or 0),
                "assistant_last_confirmation_detected": bool(row[12]),
                "application_key": _queue_identity(url),
            }
        )
    return results


def latest_jobs_db_greenhouse_batch_payload(
    *,
    jobs_db_file: str | Path = DEFAULT_JOBS_DB_FILE,
    queue_file: str | Path = DEFAULT_GREENHOUSE_ASSISTANT_QUEUE_FILE,
    mode: str = "latest_batch",
    limit: int = DEFAULT_JOBS_BATCH_LIMIT,
    skip_applied: bool = True,
) -> dict[str, Any]:
    normalized_mode = str(mode or "latest_batch").strip().lower()
    if normalized_mode not in {"latest_batch", "recent"}:
        raise CrawlFetchError("Unsupported assistant jobs mode. Use 'latest_batch' or 'recent'.")
    bounded_limit = max(1, min(int(limit or DEFAULT_JOBS_BATCH_LIMIT), MAX_JOBS_BATCH_LIMIT))

    rows = _load_jobs_rows(jobs_db_file)
    if normalized_mode == "latest_batch":
        latest_first_seen = next((str(row.get("first_seen") or "").strip() for row in rows if row.get("first_seen")), "")
        if latest_first_seen:
            rows = [row for row in rows if str(row.get("first_seen") or "").strip() == latest_first_seen]
        else:
            rows = rows[:bounded_limit]
    else:
        rows = rows[:bounded_limit]

    queue_state = _load_queue_state(queue_file)
    active_dedupe_keys = {
        str(item.get("application_key") or "").strip()
        for item in queue_state.get("items") or []
        if str(item.get("status") or "").strip().lower() in ACTIVE_QUEUE_STATUSES
    }

    candidates: list[dict[str, Any]] = []
    skipped: list[dict[str, Any]] = []
    for row in rows:
        application_status = str(row.get("application_status") or "").strip().lower()
        work_auth_status = str(row.get("work_auth_status") or "").strip().lower()
        is_applied = application_status == "applied" or bool(row.get("assistant_last_confirmation_detected"))
        dedupe_key = str(row.get("application_key") or "").strip()
        if skip_applied and is_applied:
            skipped.append(
                {
                    "requested_url": row.get("requested_url"),
                    "company_name": row.get("company_name"),
                    "job_title": row.get("job_title"),
                    "reason": "already_applied",
                }
            )
            continue
        if work_auth_status == "blocked":
            skipped.append(
                {
                    "requested_url": row.get("requested_url"),
                    "company_name": row.get("company_name"),
                    "job_title": row.get("job_title"),
                    "reason": "work_auth_blocked",
                }
            )
            continue
        if dedupe_key and dedupe_key in active_dedupe_keys:
            skipped.append(
                {
                    "requested_url": row.get("requested_url"),
                    "company_name": row.get("company_name"),
                    "job_title": row.get("job_title"),
                    "reason": "already_queued",
                }
            )
            continue
        candidates.append(row)

    return {
        "ok": True,
        "mode": normalized_mode,
        "limit": bounded_limit,
        "skip_applied": bool(skip_applied),
        "jobs_db_file": str(_resolve_path(jobs_db_file)),
        "candidate_count": len(candidates),
        "skipped_count": len(skipped),
        "candidates": candidates,
        "skipped": skipped,
    }


class GreenhouseAssistantQueue:
    def __init__(
        self,
        *,
        queue_file: str | Path = DEFAULT_GREENHOUSE_ASSISTANT_QUEUE_FILE,
        jobs_db_file: str | Path = DEFAULT_JOBS_DB_FILE,
    ) -> None:
        self._queue_file = queue_file
        self._jobs_db_file = jobs_db_file
        self._lock = threading.Lock()

    def _queue_snapshot_from_state(self, state: dict[str, Any]) -> dict[str, Any]:
        items = [
            _queue_item_payload(item)
            for item in sorted(
                list(state.get("items") or []),
                key=lambda current: (
                    str(current.get("updated_at") or ""),
                    str(current.get("added_at") or ""),
                    str(current.get("queue_id") or ""),
                ),
                reverse=True,
            )
        ]
        counts = _queue_status_counts(items)
        return {
            "ok": True,
            "updated_at": state.get("updated_at"),
            "queue_count": len(items),
            "active_count": sum(counts.get(status, 0) for status in ACTIVE_QUEUE_STATUSES),
            "counts": counts,
            "items": items,
        }

    def snapshot(self) -> dict[str, Any]:
        with self._lock:
            return self._queue_snapshot_from_state(_load_queue_state(self._queue_file))

    def jobs_batch_payload(
        self,
        *,
        mode: str = "latest_batch",
        limit: int = DEFAULT_JOBS_BATCH_LIMIT,
        skip_applied: bool = True,
    ) -> dict[str, Any]:
        with self._lock:
            return latest_jobs_db_greenhouse_batch_payload(
                jobs_db_file=self._jobs_db_file,
                queue_file=self._queue_file,
                mode=mode,
                limit=limit,
                skip_applied=skip_applied,
            )

    def _find_active_duplicate(self, items: list[dict[str, Any]], application_key: str) -> dict[str, Any] | None:
        for item in items:
            if str(item.get("application_key") or "").strip() != application_key:
                continue
            if str(item.get("status") or "").strip().lower() in ACTIVE_QUEUE_STATUSES:
                return item
        return None

    def _append_item(
        self,
        state: dict[str, Any],
        *,
        requested_url: str,
        company_name: str | None,
        job_title: str | None,
        fingerprint: str | None,
        first_seen: str | None,
        source: str | None,
        application_status: str | None,
        work_auth_status: str | None,
        assistant_last_outcome: str | None,
        assistant_last_source: str | None,
        assistant_last_review_pending_count: int = 0,
        added_by: str,
        selected_resume_variant: str | None = None,
        selected_resume_path: str | None = None,
        resume_selection_source: str | None = None,
    ) -> tuple[str, dict[str, Any] | None]:
        application_key = _queue_identity(requested_url)
        duplicate = self._find_active_duplicate(list(state.get("items") or []), application_key)
        if duplicate is not None:
            return "duplicate", duplicate

        now = utc_now()
        item = {
            "queue_id": f"ghqueue_{secrets.token_hex(6)}",
            "status": "queued",
            "requested_url": requested_url,
            "application_key": application_key,
            "company_name": str(company_name or "").strip(),
            "job_title": str(job_title or "").strip(),
            "fingerprint": str(fingerprint or "").strip(),
            "first_seen": str(first_seen or "").strip(),
            "source": str(source or "").strip(),
            "application_status": str(application_status or "").strip(),
            "work_auth_status": str(work_auth_status or "").strip(),
            "assistant_last_outcome": str(assistant_last_outcome or "").strip(),
            "assistant_last_source": str(assistant_last_source or "").strip(),
            "assistant_last_review_pending_count": max(0, int(assistant_last_review_pending_count or 0)),
            "added_by": added_by,
            "added_at": now,
            "updated_at": now,
            "run_id": "",
            "last_trace_id": "",
            "last_error": "",
            "review_pending_count": 0,
            "submitted": False,
            "confirmation_detected": False,
            "skip_reason": "",
            "selected_resume_variant": str(selected_resume_variant or "").strip(),
            "selected_resume_path": str(selected_resume_path or "").strip(),
            "resume_selection_source": str(resume_selection_source or "").strip(),
            "resume_selection_updated_at": now if selected_resume_variant or selected_resume_path else "",
        }
        state.setdefault("items", []).append(item)
        return "added", item

    def enqueue_url(
        self,
        *,
        requested_url: str,
        company_name: str | None = None,
        job_title: str | None = None,
        fingerprint: str | None = None,
        first_seen: str | None = None,
        source: str | None = None,
        application_status: str | None = None,
        work_auth_status: str | None = None,
        assistant_last_outcome: str | None = None,
        assistant_last_source: str | None = None,
        assistant_last_review_pending_count: int = 0,
        added_by: str = "manual",
        skip_applied: bool = True,
        selected_resume_variant: str | None = None,
        selected_resume_path: str | None = None,
        resume_selection_source: str | None = None,
    ) -> dict[str, Any]:
        normalized_url = str(requested_url or "").strip()
        if not normalized_url:
            raise CrawlFetchError("Payload is missing url.")
        if not _supported_greenhouse_candidate(normalized_url):
            raise CrawlFetchError("URL is not a supported hosted Greenhouse application page.")
        normalized_status = str(application_status or "").strip().lower()
        if skip_applied and normalized_status == "applied":
            return {
                "ok": True,
                "added_count": 0,
                "duplicate_count": 0,
                "skipped_count": 1,
                "added": [],
                "duplicates": [],
                "skipped": [
                    {
                        "requested_url": normalized_url,
                        "company_name": company_name,
                        "job_title": job_title,
                        "reason": "already_applied",
                    }
                ],
                "queue": self.snapshot(),
            }

        with self._lock:
            state = _load_queue_state(self._queue_file)
            outcome, item = self._append_item(
                state,
                requested_url=normalized_url,
                company_name=company_name,
                job_title=job_title,
                fingerprint=fingerprint,
                first_seen=first_seen,
                source=source,
                application_status=application_status,
                work_auth_status=work_auth_status,
                assistant_last_outcome=assistant_last_outcome,
                assistant_last_source=assistant_last_source,
                assistant_last_review_pending_count=assistant_last_review_pending_count,
                added_by=added_by,
                selected_resume_variant=selected_resume_variant,
                selected_resume_path=selected_resume_path,
                resume_selection_source=resume_selection_source,
            )
            if outcome == "added":
                _save_queue_state(self._queue_file, state)
            snapshot = self._queue_snapshot_from_state(state)

        return {
            "ok": True,
            "added_count": 1 if outcome == "added" else 0,
            "duplicate_count": 1 if outcome == "duplicate" else 0,
            "skipped_count": 0,
            "added": [_queue_item_payload(item)] if outcome == "added" and item else [],
            "duplicates": [_queue_item_payload(item)] if outcome == "duplicate" and item else [],
            "skipped": [],
            "queue": snapshot,
        }

    def enqueue_latest_jobs(
        self,
        *,
        mode: str = "latest_batch",
        limit: int = DEFAULT_JOBS_BATCH_LIMIT,
        skip_applied: bool = True,
        added_by: str = "jobs_db_batch",
        selection_by_application_key: dict[str, dict[str, Any]] | None = None,
    ) -> dict[str, Any]:
        batch = self.jobs_batch_payload(mode=mode, limit=limit, skip_applied=skip_applied)
        added: list[dict[str, Any]] = []
        duplicates: list[dict[str, Any]] = []
        skipped = list(batch.get("skipped") or [])

        with self._lock:
            state = _load_queue_state(self._queue_file)
            for candidate in batch.get("candidates") or []:
                application_key = str(candidate.get("application_key") or "").strip()
                selection = selection_by_application_key.get(application_key, {}) if selection_by_application_key else {}
                outcome, item = self._append_item(
                    state,
                    requested_url=str(candidate.get("requested_url") or "").strip(),
                    company_name=str(candidate.get("company_name") or "").strip() or None,
                    job_title=str(candidate.get("job_title") or "").strip() or None,
                    fingerprint=str(candidate.get("fingerprint") or "").strip() or None,
                    first_seen=str(candidate.get("first_seen") or "").strip() or None,
                    source=str(candidate.get("source") or "").strip() or None,
                    application_status=str(candidate.get("application_status") or "").strip() or None,
                    work_auth_status=str(candidate.get("work_auth_status") or "").strip() or None,
                    assistant_last_outcome=str(candidate.get("assistant_last_outcome") or "").strip() or None,
                    assistant_last_source=str(candidate.get("assistant_last_source") or "").strip() or None,
                    assistant_last_review_pending_count=int(candidate.get("assistant_last_review_pending_count") or 0),
                    added_by=added_by,
                    selected_resume_variant=str(selection.get("selected_resume_variant") or "").strip() or None,
                    selected_resume_path=str(selection.get("selected_resume_path") or "").strip() or None,
                    resume_selection_source=str(selection.get("resume_selection_source") or "").strip() or None,
                )
                if outcome == "added" and item is not None:
                    added.append(_queue_item_payload(item))
                elif outcome == "duplicate" and item is not None:
                    duplicates.append(_queue_item_payload(item))
            if added:
                _save_queue_state(self._queue_file, state)
            snapshot = self._queue_snapshot_from_state(state)

        return {
            "ok": True,
            "mode": batch.get("mode"),
            "limit": batch.get("limit"),
            "skip_applied": batch.get("skip_applied"),
            "candidate_count": batch.get("candidate_count"),
            "added_count": len(added),
            "duplicate_count": len(duplicates),
            "skipped_count": len(skipped),
            "added": added,
            "duplicates": duplicates,
            "skipped": skipped,
            "queue": snapshot,
        }

    def claim_next(self, *, run_id: str) -> dict[str, Any]:
        with self._lock:
            state = _load_queue_state(self._queue_file)
            items = state.get("items") or []
            next_item: dict[str, Any] | None = None
            for item in items:
                if str(item.get("status") or "").strip().lower() == "queued":
                    next_item = item
                    break
            if next_item is None:
                raise CrawlFetchError("Assistant queue is empty.")
            next_item["status"] = "running"
            next_item["run_id"] = run_id
            next_item["updated_at"] = utc_now()
            next_item["last_error"] = ""
            _save_queue_state(self._queue_file, state)
            return _queue_item_payload(next_item)

    def mark_finished(
        self,
        *,
        queue_id: str,
        run_id: str,
        result_summary: dict[str, Any] | None = None,
        error: str | None = None,
    ) -> None:
        normalized_queue_id = str(queue_id or "").strip()
        if not normalized_queue_id:
            return
        with self._lock:
            state = _load_queue_state(self._queue_file)
            items = state.get("items") or []
            target = next(
                (item for item in items if str(item.get("queue_id") or "").strip() == normalized_queue_id),
                None,
            )
            if target is None:
                return
            target["run_id"] = str(run_id or "").strip()
            target["updated_at"] = utc_now()
            if error:
                target["status"] = "failed"
                target["last_error"] = str(error).strip()
                _save_queue_state(self._queue_file, state)
                return

            summary = result_summary or {}
            review_pending_count = max(0, int(summary.get("review_pending_count") or 0))
            submitted = bool(summary.get("submitted"))
            confirmation_detected = bool(summary.get("confirmation_detected"))
            auto_submit_eligible = bool(summary.get("auto_submit_eligible"))
            eligible_after_browser_validation = bool(summary.get("eligible_after_browser_validation"))
            challenge_detected = bool(summary.get("challenge_detected"))

            target["review_pending_count"] = review_pending_count
            target["submitted"] = submitted
            target["confirmation_detected"] = confirmation_detected
            target["last_trace_id"] = str(summary.get("trace_id") or "").strip()
            target["last_error"] = ""
            if str(summary.get("selected_resume_variant") or "").strip():
                target["selected_resume_variant"] = str(summary.get("selected_resume_variant") or "").strip()
                target["resume_selection_updated_at"] = utc_now()
            if str(summary.get("selected_resume_path") or "").strip():
                target["selected_resume_path"] = str(summary.get("selected_resume_path") or "").strip()
                target["resume_selection_updated_at"] = utc_now()
            if str(summary.get("resume_selection_source") or "").strip():
                target["resume_selection_source"] = str(summary.get("resume_selection_source") or "").strip()
            if submitted or confirmation_detected:
                target["status"] = "completed"
                target["application_status"] = "applied"
            elif review_pending_count > 0 or challenge_detected:
                target["status"] = "review"
            elif auto_submit_eligible or eligible_after_browser_validation:
                target["status"] = "ready"
            else:
                target["status"] = "completed"
            _save_queue_state(self._queue_file, state)

    def update_resume_selection(
        self,
        *,
        queue_id: str | None = None,
        requested_url: str | None = None,
        selected_resume_variant: str | None = None,
        selected_resume_path: str | None = None,
        resume_selection_source: str | None = None,
    ) -> dict[str, Any]:
        normalized_queue_id = str(queue_id or "").strip()
        normalized_requested_url = str(requested_url or "").strip()
        if not normalized_queue_id and not normalized_requested_url:
            raise CrawlFetchError("Queue update requires queue_id or requested_url.")

        with self._lock:
            state = _load_queue_state(self._queue_file)
            items = state.get("items") or []
            target: dict[str, Any] | None = None
            if normalized_queue_id:
                target = next(
                    (item for item in items if str(item.get("queue_id") or "").strip() == normalized_queue_id),
                    None,
                )
            elif normalized_requested_url:
                application_key = _queue_identity(normalized_requested_url)
                target = self._find_active_duplicate(items, application_key)
            if target is None:
                raise CrawlFetchError("Assistant queue item not found.")

            target["selected_resume_variant"] = str(selected_resume_variant or "").strip()
            target["selected_resume_path"] = str(selected_resume_path or "").strip()
            target["resume_selection_source"] = str(resume_selection_source or "").strip() or "manual_override"
            target["resume_selection_updated_at"] = utc_now()
            target["updated_at"] = target["resume_selection_updated_at"]
            _save_queue_state(self._queue_file, state)
            snapshot = self._queue_snapshot_from_state(state)

        return {
            "ok": True,
            "updated": [_queue_item_payload(target)],
            "queue": snapshot,
        }
