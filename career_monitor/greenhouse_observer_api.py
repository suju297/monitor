from __future__ import annotations

import argparse
import copy
import json
import secrets
import threading
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlparse

from .constants import DEFAULT_TIMEOUT_SECONDS
from .exceptions import CrawlFetchError
from .greenhouse_assistant import (
    build_approved_answer_targets,
    build_autofill_targets,
    build_suggested_answer_targets,
    detect_greenhouse_application,
    inspect_greenhouse_application,
    recommend_resume_selection,
    resume_variant_options,
)
from .greenhouse_playwright import (
    DEFAULT_GREENHOUSE_STORAGE_STATE_FILE,
    execute_greenhouse_autofill,
)
from .greenhouse_assistant_queue import (
    DEFAULT_GREENHOUSE_ASSISTANT_QUEUE_FILE,
    DEFAULT_JOBS_BATCH_LIMIT,
    GreenhouseAssistantQueue,
)
from .jobs_db_sync import DEFAULT_JOBS_DB_FILE, sync_greenhouse_job_record
from .greenhouse_submission_state import build_greenhouse_application_key
from .greenhouse_trace_store import (
    DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE,
    DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
    DEFAULT_GREENHOUSE_TRACE_FILE,
    DEFAULT_GREENHOUSE_TRAINING_EXAMPLES_FILE,
    append_greenhouse_manual_observation,
)
from .greenhouse_learning_summary import (
    DEFAULT_LEARNING_LIMIT,
    MAX_LEARNING_LIMIT,
    greenhouse_learning_summary_payload,
)
from .utils import utc_now

DEFAULT_OBSERVER_API_HOST = "127.0.0.1"
DEFAULT_OBSERVER_API_PORT = 8776
DEFAULT_HISTORY_LIMIT = 5
MAX_HISTORY_LIMIT = 20
DEFAULT_EVENT_LIMIT = 80
MAX_EVENT_LIMIT = 200
DEFAULT_ASSISTANT_HISTORY_LIMIT = 10
MAX_ASSISTANT_HISTORY_LIMIT = 25


def _load_json_object_file(path: str | None) -> dict[str, Any]:
    if not path:
        return {}
    payload = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise CrawlFetchError("Configured JSON file must contain an object.")
    return payload


def _read_jsonl_records(path: str | Path) -> list[dict[str, Any]]:
    record_path = Path(path).expanduser()
    if not record_path.is_absolute():
        record_path = Path.cwd() / record_path
    if not record_path.exists():
        return []
    records: list[dict[str, Any]] = []
    for line in record_path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        payload = json.loads(line)
        if isinstance(payload, dict):
            records.append(payload)
    return records


def _coerce_limit(raw_value: str | None, *, default: int, maximum: int) -> int:
    try:
        value = int(str(raw_value or "").strip())
    except Exception:
        return default
    if value <= 0:
        return default
    return min(value, maximum)


def _application_key_for_url(url: str) -> str | None:
    detection = detect_greenhouse_application(url)
    if not detection.is_greenhouse or not detection.is_application:
        return None
    return build_greenhouse_application_key(
        board_token=detection.board_token,
        job_id=detection.job_id,
        public_url=url,
    )


def _profile_with_resume_selection(
    profile: dict[str, Any],
    *,
    selected_resume_variant: str | None,
    selected_resume_path: str | None,
    resume_selection_source: str | None,
    resume_selection_reason: str | None = None,
    resume_selection_confidence: float | None = None,
) -> dict[str, Any]:
    merged = dict(profile)
    if selected_resume_variant:
        merged["selected_resume_variant"] = selected_resume_variant
    if selected_resume_path:
        merged["selected_resume_path"] = selected_resume_path
        merged["resume_path"] = selected_resume_path
    if resume_selection_source:
        merged["resume_selection_source"] = resume_selection_source
    if resume_selection_reason:
        merged["resume_selection_reason"] = resume_selection_reason
    if resume_selection_confidence is not None:
        merged["resume_selection_confidence"] = resume_selection_confidence
    return merged


def _resume_selection_from_profile(
    url: str,
    *,
    profile: dict[str, Any],
    approved_answers: dict[str, Any] | None,
    timeout_seconds: int,
) -> tuple[Any, dict[str, Any], dict[str, Any]]:
    analysis = inspect_greenhouse_application(
        url,
        profile=profile,
        approved_answers=approved_answers,
        timeout_seconds=timeout_seconds,
    )
    selection = recommend_resume_selection(profile, analysis.schema)
    resolved_profile = _profile_with_resume_selection(
        profile,
        selected_resume_variant=selection.variant,
        selected_resume_path=selection.path,
        resume_selection_source=selection.source,
        resume_selection_reason=selection.reason,
        resume_selection_confidence=selection.confidence,
    )
    return (
        analysis,
        resolved_profile,
        {
            "available_resume_variants": selection.available_variants,
            "recommended_resume_variant": selection.variant,
            "recommended_resume_path": selection.path,
            "recommended_resume_file": Path(selection.path).name if selection.path else None,
            "recommended_resume_reason": selection.reason,
            "recommended_resume_confidence": selection.confidence,
            "resume_selection_source": selection.source,
        },
    )


def _assistant_config_payload(profile: dict[str, Any]) -> dict[str, Any]:
    return {
        "ok": True,
        "available_resume_variants": resume_variant_options(profile),
        "default_resume_variant": str(profile.get("resume_variant_default") or "").strip() or None,
    }


def _session_summary(record: dict[str, Any]) -> dict[str, Any]:
    observation = record.get("observation_summary") or {}
    assistant = record.get("assistant_recommendation") or {}
    manual_resume = record.get("manual_resume_decision") or {}
    return {
        "manual_session_id": record.get("manual_session_id"),
        "captured_at": record.get("captured_at"),
        "application_key": record.get("application_key"),
        "company_name": record.get("company_name"),
        "job_title": record.get("job_title"),
        "requested_url": record.get("requested_url"),
        "public_url": record.get("public_url"),
        "final_page_url": record.get("final_page_url"),
        "submitted": bool(record.get("submitted")),
        "confirmation_detected": bool(record.get("confirmation_detected")),
        "ended_reason": record.get("ended_reason"),
        "schema_source": record.get("schema_source"),
        "observation_summary": {
            "event_count": observation.get("event_count", 0),
            "touched_question_count": observation.get("touched_question_count", 0),
            "final_answer_count": observation.get("final_answer_count", 0),
            "matched_recommendation_count": observation.get("matched_recommendation_count", 0),
            "override_count": observation.get("override_count", 0),
            "manual_only_answer_count": observation.get("manual_only_answer_count", 0),
            "queue_answer_count": observation.get("queue_answer_count", 0),
            "review_answer_count": observation.get("review_answer_count", 0),
        },
        "assistant_recommendation": {
            "resume_file": assistant.get("resume_file"),
            "resume_variant": assistant.get("resume_variant"),
        },
        "manual_resume_decision": {
            "selected_file": manual_resume.get("selected_file"),
            "selected_variant": manual_resume.get("selected_variant"),
            "decision_source": manual_resume.get("decision_source"),
            "external_recommendation": manual_resume.get("external_recommendation"),
            "matches_assistant_recommendation": manual_resume.get("matches_assistant_recommendation"),
        },
    }


def recent_manual_sessions_payload(
    url: str,
    *,
    session_file: str | Path,
    limit: int = DEFAULT_HISTORY_LIMIT,
) -> dict[str, Any]:
    requested_url = str(url or "").strip()
    if not requested_url:
        raise CrawlFetchError("Missing url query parameter.")
    application_key = _application_key_for_url(requested_url)
    records = [
        record
        for record in _read_jsonl_records(session_file)
        if record.get("record_type") == "greenhouse_manual_session"
    ]
    if application_key:
        records = [record for record in records if record.get("application_key") == application_key]
    else:
        records = [
            record
            for record in records
            if requested_url
            in {
                str(record.get("requested_url") or "").strip(),
                str(record.get("public_url") or "").strip(),
                str(record.get("final_page_url") or "").strip(),
            }
        ]
    records.sort(key=lambda item: str(item.get("captured_at") or ""), reverse=True)
    limited = records[:limit]
    return {
        "ok": True,
        "requested_url": requested_url,
        "application_key": application_key,
        "limit": limit,
        "session_count": len(limited),
        "sessions": [_session_summary(record) for record in limited],
    }


def manual_session_detail_payload(
    manual_session_id: str,
    *,
    session_file: str | Path,
    field_events_file: str | Path,
    event_limit: int = DEFAULT_EVENT_LIMIT,
) -> dict[str, Any]:
    session_id = str(manual_session_id or "").strip()
    if not session_id:
        raise CrawlFetchError("Missing manual_session_id query parameter.")
    session_records = [
        record
        for record in _read_jsonl_records(session_file)
        if record.get("record_type") == "greenhouse_manual_session"
        and str(record.get("manual_session_id") or "").strip() == session_id
    ]
    if not session_records:
        raise CrawlFetchError(f"Manual session not found: {session_id}")
    session_record = session_records[-1]

    event_records = [
        record
        for record in _read_jsonl_records(field_events_file)
        if record.get("record_type") == "greenhouse_manual_field_event"
        and str(record.get("manual_session_id") or "").strip() == session_id
    ]
    event_records.sort(
        key=lambda item: (
            int(item.get("sequence") or 0),
            int(item.get("offset_ms") or 0),
        )
    )
    limited_events = event_records[-event_limit:] if event_limit else event_records
    return {
        "ok": True,
        "manual_session_id": session_id,
        "event_limit": event_limit,
        "event_count_total": len(event_records),
        "event_count_returned": len(limited_events),
        "session": session_record,
        "events": limited_events,
    }


def _assistant_run_summary(result: dict[str, Any]) -> dict[str, Any]:
    analysis = result.get("analysis") or {}
    review_queue = analysis.get("review_queue") or []
    submission = result.get("submission") or {}
    submit_safety = result.get("submit_safety") or {}
    resume_selection = result.get("resume_selection") or {}
    trace = result.get("trace") or {}
    manual_observation = result.get("manual_observation") or {}
    jobs_db_sync = result.get("jobs_db_sync") or {}
    return {
        "company_name": str((analysis.get("schema") or {}).get("company_name") or "").strip() or None,
        "job_title": str((analysis.get("schema") or {}).get("title") or "").strip() or None,
        "filled_count": len(result.get("filled") or []),
        "skipped_count": len(result.get("skipped") or []),
        "error_count": len(result.get("errors") or []),
        "browser_validation_error_count": len(result.get("browser_validation_errors") or []),
        "review_pending_count": sum(
            1 for item in review_queue if str(item.get("status") or "").strip() in {"pending", "blocked"}
        ),
        "auto_submit_eligible": bool(analysis.get("auto_submit_eligible")),
        "eligible_after_browser_validation": bool(submit_safety.get("eligible_after_browser_validation")),
        "submitted": bool(submission.get("submitted")),
        "confirmation_detected": bool(submission.get("confirmation_detected")),
        "job_blocker_count": len(submit_safety.get("job_blockers") or []),
        "challenge_detected": bool(submit_safety.get("challenge_detected")),
        "challenge_blocker_count": len(submit_safety.get("challenge_blockers") or []),
        "selected_resume_variant": str(resume_selection.get("selected_resume_variant") or "").strip() or None,
        "selected_resume_file": str(resume_selection.get("selected_resume_file") or "").strip() or None,
        "resume_selection_source": str(resume_selection.get("resume_selection_source") or "").strip() or None,
        "trace_id": str(trace.get("trace_id") or "").strip() or None,
        "manual_session_id": str(manual_observation.get("manual_session_id") or "").strip() or None,
        "manual_event_count": int(manual_observation.get("field_event_record_count") or 0),
        "jobs_db_stored": bool(jobs_db_sync.get("stored")),
    }


def _assistant_history_summary(record: dict[str, Any]) -> dict[str, Any]:
    result_summary = record.get("result_summary") or {}
    return {
        "trace_id": record.get("trace_id"),
        "captured_at": record.get("captured_at"),
        "application_key": record.get("application_key"),
        "company_name": record.get("company_name"),
        "job_title": record.get("job_title"),
        "requested_url": record.get("requested_url"),
        "public_url": record.get("public_url"),
        "page_url": record.get("page_url"),
        "schema_source": record.get("schema_source"),
        "run_resume_selection": record.get("run_resume_selection") or None,
        "result_summary": {
            "filled_count": result_summary.get("filled_count", 0),
            "skipped_count": result_summary.get("skipped_count", 0),
            "error_count": result_summary.get("error_count", 0),
            "review_queue_count": result_summary.get("review_queue_count", 0),
            "review_queue_status_counts": result_summary.get("review_queue_status_counts", {}),
            "auto_submit_eligible": bool(result_summary.get("auto_submit_eligible")),
            "eligible_after_browser_validation": bool(result_summary.get("eligible_after_browser_validation")),
            "submission_requested": bool(result_summary.get("submission_requested")),
            "submission_attempted": bool(result_summary.get("submission_attempted")),
            "submitted": bool(result_summary.get("submitted")),
            "confirmation_detected": bool(result_summary.get("confirmation_detected")),
        },
    }


def recent_assistant_runs_payload(
    url: str,
    *,
    trace_file: str | Path = DEFAULT_GREENHOUSE_TRACE_FILE,
    limit: int = DEFAULT_ASSISTANT_HISTORY_LIMIT,
) -> dict[str, Any]:
    requested_url = str(url or "").strip()
    if not requested_url:
        raise CrawlFetchError("Missing url query parameter.")
    application_key = _application_key_for_url(requested_url)
    records = [
        record
        for record in _read_jsonl_records(trace_file)
        if record.get("record_type") == "greenhouse_run_trace"
    ]
    if application_key:
        records = [record for record in records if record.get("application_key") == application_key]
    else:
        records = [
            record
            for record in records
            if requested_url
            in {
                str(record.get("requested_url") or "").strip(),
                str(record.get("public_url") or "").strip(),
                str(record.get("page_url") or "").strip(),
            }
        ]
    records.sort(key=lambda item: str(item.get("captured_at") or ""), reverse=True)
    limited = records[:limit]
    return {
        "ok": True,
        "requested_url": requested_url,
        "application_key": application_key,
        "limit": limit,
        "run_count": len(limited),
        "runs": [_assistant_history_summary(record) for record in limited],
    }


def assistant_trace_detail_payload(
    trace_id: str,
    *,
    trace_file: str | Path = DEFAULT_GREENHOUSE_TRACE_FILE,
) -> dict[str, Any]:
    normalized_trace_id = str(trace_id or "").strip()
    if not normalized_trace_id:
        raise CrawlFetchError("Missing trace_id query parameter.")
    records = [
        record
        for record in _read_jsonl_records(trace_file)
        if record.get("record_type") == "greenhouse_run_trace"
        and str(record.get("trace_id") or "").strip() == normalized_trace_id
    ]
    if not records:
        raise CrawlFetchError(f"Assistant trace not found: {normalized_trace_id}")
    record = records[-1]
    return {
        "ok": True,
        "trace_id": normalized_trace_id,
        "trace": record,
    }


class AssistantRunManager:
    def __init__(
        self,
        *,
        profile_file: str | None,
        answers_file: str | None,
        timeout_seconds: int,
        trace_file: str,
        training_examples_file: str,
        storage_state_file: str,
        sync_jobs_db: bool,
        jobs_db_file: str,
        queue_manager: GreenhouseAssistantQueue | None = None,
    ) -> None:
        self._profile_file = profile_file
        self._answers_file = answers_file
        self._timeout_seconds = timeout_seconds
        self._trace_file = trace_file
        self._training_examples_file = training_examples_file
        self._storage_state_file = storage_state_file
        self._sync_jobs_db = sync_jobs_db
        self._jobs_db_file = jobs_db_file
        self._queue_manager = queue_manager
        self._lock = threading.Lock()
        self._runs: dict[str, dict[str, Any]] = {}

    def _snapshot(self, run_id: str) -> dict[str, Any]:
        with self._lock:
            record = self._runs.get(run_id)
            if record is None:
                raise CrawlFetchError(f"Assistant run not found: {run_id}")
            return copy.deepcopy(record)

    def get_run(self, run_id: str) -> dict[str, Any]:
        snapshot = self._snapshot(run_id)
        snapshot["ok"] = True
        return snapshot

    def list_runs(self) -> dict[str, Any]:
        with self._lock:
            runs = [copy.deepcopy(record) for record in self._runs.values()]
        runs.sort(
            key=lambda item: (
                str(item.get("status") or "") not in {"running", "queued"},
                str(item.get("started_at") or item.get("created_at") or ""),
                str(item.get("run_id") or ""),
            ),
            reverse=True,
        )
        return {
            "ok": True,
            "run_count": len(runs),
            "runs": runs,
        }

    def start_run(
        self,
        *,
        url: str,
        allow_submit: bool = False,
        headless: bool = False,
        queue_id: str | None = None,
        resume_variant: str | None = None,
        resume_path: str | None = None,
        resume_selection_source: str | None = None,
    ) -> dict[str, Any]:
        requested_url = str(url or "").strip()
        if not requested_url:
            raise CrawlFetchError("Payload is missing url.")
        detection = detect_greenhouse_application(requested_url)
        if not detection.is_greenhouse or not detection.is_application:
            raise CrawlFetchError("URL is not a supported Greenhouse application page.")

        with self._lock:
            for record in self._runs.values():
                if (
                    record.get("requested_url") == requested_url
                    and str(record.get("status") or "") in {"queued", "running"}
                ):
                    existing = copy.deepcopy(record)
                    existing["ok"] = True
                    existing["reused_existing_run"] = True
                    return existing

            run_id = f"ghrun_{secrets.token_hex(8)}"
            record = {
                "run_id": run_id,
                "status": "queued",
                "requested_url": requested_url,
                "allow_submit": bool(allow_submit),
                "headless": bool(headless),
                "queue_id": str(queue_id or "").strip() or None,
                "selected_resume_variant": str(resume_variant or "").strip() or None,
                "selected_resume_path": str(resume_path or "").strip() or None,
                "resume_selection_source": str(resume_selection_source or "").strip() or None,
                "created_at": utc_now(),
                "started_at": None,
                "finished_at": None,
                "error": None,
                "result_summary": None,
                "result": None,
            }
            self._runs[run_id] = record

        worker = threading.Thread(
            target=self._execute_run,
            args=(run_id,),
            name=f"greenhouse-assistant-{run_id}",
            daemon=True,
        )
        worker.start()
        return self.get_run(run_id)

    def start_next_queued_run(
        self,
        *,
        allow_submit: bool = False,
        headless: bool = False,
    ) -> dict[str, Any]:
        if self._queue_manager is None:
            raise CrawlFetchError("Assistant queue is not configured.")
        run_id = f"ghrun_{secrets.token_hex(8)}"
        queue_item = self._queue_manager.claim_next(run_id=run_id)
        requested_url = str(queue_item.get("requested_url") or "").strip()
        if not requested_url:
            raise CrawlFetchError("Queued assistant item is missing a URL.")

        with self._lock:
            record = {
                "run_id": run_id,
                "status": "queued",
                "requested_url": requested_url,
                "allow_submit": bool(allow_submit),
                "headless": bool(headless),
                "queue_id": str(queue_item.get("queue_id") or "").strip() or None,
                "selected_resume_variant": str(queue_item.get("selected_resume_variant") or "").strip() or None,
                "selected_resume_path": str(queue_item.get("selected_resume_path") or "").strip() or None,
                "resume_selection_source": str(queue_item.get("resume_selection_source") or "").strip() or None,
                "created_at": utc_now(),
                "started_at": None,
                "finished_at": None,
                "error": None,
                "result_summary": None,
                "result": None,
            }
            self._runs[run_id] = record

        worker = threading.Thread(
            target=self._execute_run,
            args=(run_id,),
            name=f"greenhouse-assistant-{run_id}",
            daemon=True,
        )
        worker.start()
        return self.get_run(run_id)

    def _execute_run(self, run_id: str) -> None:
        with self._lock:
            record = self._runs.get(run_id)
            if record is None:
                return
            record["status"] = "running"
            record["started_at"] = utc_now()
            requested_url = str(record.get("requested_url") or "").strip()
            allow_submit = bool(record.get("allow_submit"))
            headless = bool(record.get("headless"))
            queue_id = str(record.get("queue_id") or "").strip()
            selected_resume_variant = str(record.get("selected_resume_variant") or "").strip() or None
            selected_resume_path = str(record.get("selected_resume_path") or "").strip() or None
            resume_selection_source = str(record.get("resume_selection_source") or "").strip() or None

        try:
            profile = _load_json_object_file(self._profile_file)
            if selected_resume_variant or selected_resume_path:
                profile = _profile_with_resume_selection(
                    profile,
                    selected_resume_variant=selected_resume_variant,
                    selected_resume_path=selected_resume_path,
                    resume_selection_source=resume_selection_source or "manual_override",
                )
            approved_answers = _load_json_object_file(self._answers_file) if self._answers_file else None
            result = execute_greenhouse_autofill(
                requested_url,
                profile=profile,
                approved_answers=approved_answers,
                headless=headless,
                timeout_seconds=self._timeout_seconds,
                allow_submit=allow_submit,
                keep_open_for_review=not headless,
                storage_state_file=self._storage_state_file,
                store_traces=True,
                trace_file=self._trace_file,
                training_examples_file=self._training_examples_file,
                sync_jobs_db=self._sync_jobs_db,
                jobs_db_file=self._jobs_db_file,
                reuse_browser_session=True,
            )
            result_payload = result.to_dict()
            result_summary = _assistant_run_summary(result_payload)
        except Exception as exc:  # noqa: BLE001
            with self._lock:
                record = self._runs.get(run_id)
                if record is None:
                    return
                record["status"] = "failed"
                record["finished_at"] = utc_now()
                record["error"] = str(exc)
            if self._queue_manager is not None and queue_id:
                self._queue_manager.mark_finished(
                    queue_id=queue_id,
                    run_id=run_id,
                    error=str(exc),
                )
            return

        with self._lock:
            record = self._runs.get(run_id)
            if record is None:
                return
            if (
                result_summary.get("challenge_detected")
                or int(result_summary.get("review_pending_count") or 0) > 0
                or int(result_summary.get("job_blocker_count") or 0) > 0
            ):
                record["status"] = "review"
            else:
                record["status"] = "completed"
            record["finished_at"] = utc_now()
            record["error"] = None
            record["result_summary"] = result_summary
            record["result"] = result_payload
        if self._queue_manager is not None and queue_id:
            self._queue_manager.mark_finished(
                queue_id=queue_id,
                run_id=run_id,
                result_summary=result_summary,
            )


def _match_resume_variant(profile: dict[str, Any], path: str | None) -> str | None:
    if not path:
        return None
    raw = profile.get("resume_variants")
    if not isinstance(raw, dict):
        return None
    candidate = str(path).strip()
    candidate_path = str(Path(candidate).expanduser())
    basename = Path(candidate).name.strip().lower()
    basename_matches: list[str] = []
    for key, value in raw.items():
        if isinstance(value, dict):
            candidate = str(value.get("path") or "").strip()
        else:
            candidate = str(value or "").strip()
        if not candidate:
            continue
        variant_key = str(key).strip() or None
        if str(Path(candidate).expanduser()) == candidate_path:
            return variant_key
        if Path(candidate).name.strip().lower() == basename and variant_key:
            basename_matches.append(variant_key)
    if len(basename_matches) == 1:
        return basename_matches[0]
    return None


def _recommendation_payload(
    url: str,
    *,
    profile: dict[str, Any],
    approved_answers: dict[str, Any] | None,
    timeout_seconds: int,
) -> dict[str, Any]:
    analysis, resolved_profile, resume_selection = _resume_selection_from_profile(
        url,
        profile=profile,
        approved_answers=approved_answers,
        timeout_seconds=timeout_seconds,
    )
    fill_targets = build_autofill_targets(analysis, profile=resolved_profile)
    fill_targets.extend(
        build_approved_answer_targets(
            analysis,
            approved_answers=approved_answers,
            profile=resolved_profile,
        )
    )
    fill_targets.extend(build_suggested_answer_targets(analysis))
    return {
        "ok": True,
        "requested_url": url,
        "company_name": analysis.schema.company_name,
        "job_title": analysis.schema.title,
        "decision_counts": dict(analysis.decision_counts),
        "auto_submit_eligible": bool(analysis.auto_submit_eligible),
        "available_resume_variants": resume_selection.get("available_resume_variants") or [],
        "recommended_resume_file": resume_selection.get("recommended_resume_file"),
        "recommended_resume_path": resume_selection.get("recommended_resume_path"),
        "recommended_resume_variant": resume_selection.get("recommended_resume_variant"),
        "recommended_resume_reason": resume_selection.get("recommended_resume_reason"),
        "recommended_resume_confidence": resume_selection.get("recommended_resume_confidence"),
        "resume_selection_source": resume_selection.get("resume_selection_source"),
        "analysis": analysis.to_dict(),
        "review_queue": [
            {
                "question_label": item.question_label,
                "decision": item.decision,
                "status": item.status,
                "reason": item.reason,
                "suggested_answer": item.suggested_answer,
                "suggested_answer_source": item.suggested_answer_source,
                "suggested_answer_reason": item.suggested_answer_reason,
                "suggested_answer_confidence": item.suggested_answer_confidence,
                "draft_source": item.draft_source,
                "retrieved_chunk_ids": list(item.retrieved_chunk_ids),
                "style_snippet_ids": list(item.style_snippet_ids),
                "retrieval_summary": list(item.retrieval_summary),
            }
            for item in analysis.review_queue
        ],
    }


def _queue_resume_selection_payload(
    url: str,
    *,
    profile: dict[str, Any],
    approved_answers: dict[str, Any] | None,
    timeout_seconds: int,
) -> dict[str, Any]:
    _analysis, _resolved_profile, selection = _resume_selection_from_profile(
        url,
        profile=profile,
        approved_answers=approved_answers,
        timeout_seconds=timeout_seconds,
    )
    return {
        "selected_resume_variant": selection.get("recommended_resume_variant"),
        "selected_resume_path": selection.get("recommended_resume_path"),
        "resume_selection_source": selection.get("resume_selection_source"),
    }


def ingest_extension_observation(
    payload: dict[str, Any],
    *,
    profile: dict[str, Any],
    approved_answers: dict[str, Any] | None,
    timeout_seconds: int,
    session_file: str,
    field_events_file: str,
    sync_jobs_db: bool = True,
    jobs_db_file: str = DEFAULT_JOBS_DB_FILE,
) -> dict[str, Any]:
    url = str(payload.get("url") or "").strip()
    if not url:
        raise CrawlFetchError("Payload is missing url.")
    raw_events = payload.get("events")
    if not isinstance(raw_events, list):
        raise CrawlFetchError("Payload field 'events' must be a list.")
    events = [event for event in raw_events if isinstance(event, dict)]

    analysis = inspect_greenhouse_application(
        url,
        profile=profile,
        approved_answers=approved_answers,
        timeout_seconds=timeout_seconds,
    )
    fill_targets = build_autofill_targets(analysis, profile=profile)
    fill_targets.extend(
        build_approved_answer_targets(
            analysis,
            approved_answers=approved_answers,
            profile=profile,
        )
    )
    application_key = build_greenhouse_application_key(
        board_token=analysis.schema.detection.board_token,
        job_id=analysis.schema.detection.job_id,
        public_url=analysis.schema.public_url or url,
    )
    trace = append_greenhouse_manual_observation(
        analysis=analysis,
        observed_events=events,
        profile=profile,
        fill_targets=fill_targets,
        application_key=application_key,
        requested_url=url,
        final_page_url=str(payload.get("final_page_url") or url).strip() or url,
        confirmation_detected=bool(payload.get("confirmation_detected")),
        ended_reason=str(payload.get("ended_reason") or "").strip() or None,
        resume_decision_source=str(payload.get("resume_decision_source") or "").strip() or None,
        external_resume_recommendation=str(payload.get("external_resume_recommendation") or "").strip() or None,
        session_file=session_file,
        field_events_file=field_events_file,
    )
    pending_review_count = sum(
        1 for item in analysis.review_queue if item.status in {"pending", "blocked"}
    )
    jobs_db_sync = sync_greenhouse_job_record(
        requested_url=url,
        public_url=analysis.schema.public_url,
        source="extension_observer",
        outcome="submitted" if bool(payload.get("confirmation_detected")) else "observed",
        application_status="applied" if bool(payload.get("confirmation_detected")) else None,
        manual_session_id=str((trace or {}).get("manual_session_id") or "").strip() or None,
        auto_submit_eligible=bool(analysis.auto_submit_eligible),
        review_pending_count=pending_review_count,
        confirmation_detected=bool(payload.get("confirmation_detected")),
        jobs_db_file=jobs_db_file,
        enabled=sync_jobs_db,
    )
    return {
        "ok": True,
        "ingested_at": utc_now(),
        "company_name": analysis.schema.company_name,
        "job_title": analysis.schema.title,
        "event_count": len(events),
        "trace": trace,
        "jobs_db_sync": jobs_db_sync,
    }


def _json_response(handler: BaseHTTPRequestHandler, status: int, payload: dict[str, Any]) -> None:
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Access-Control-Allow-Origin", "*")
    handler.send_header("Access-Control-Allow-Headers", "Content-Type")
    handler.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


def make_observer_handler(
    *,
    profile_file: str | None,
    answers_file: str | None,
    timeout_seconds: int,
    session_file: str,
    field_events_file: str,
    trace_file: str,
    training_examples_file: str,
    storage_state_file: str,
    queue_file: str,
    sync_jobs_db: bool,
    jobs_db_file: str,
):
    assistant_queue = GreenhouseAssistantQueue(
        queue_file=queue_file,
        jobs_db_file=jobs_db_file,
    )
    assistant_runs = AssistantRunManager(
        profile_file=profile_file,
        answers_file=answers_file,
        timeout_seconds=timeout_seconds,
        trace_file=trace_file,
        training_examples_file=training_examples_file,
        storage_state_file=storage_state_file,
        sync_jobs_db=sync_jobs_db,
        jobs_db_file=jobs_db_file,
        queue_manager=assistant_queue,
    )

    class ObserverHandler(BaseHTTPRequestHandler):
        def do_OPTIONS(self) -> None:  # noqa: N802
            _json_response(self, 204, {"ok": True})

        def do_GET(self) -> None:  # noqa: N802
            parsed = urlparse(self.path)
            if parsed.path == "/api/health":
                _json_response(self, 200, {"ok": True, "time": utc_now()})
                return
            if parsed.path not in {
                "/api/greenhouse-observer/recommendation",
                "/api/greenhouse-observer/history",
                "/api/greenhouse-observer/session",
                "/api/greenhouse-observer/assistant/config",
                "/api/greenhouse-observer/assistant/run",
                "/api/greenhouse-observer/assistant/runs",
                "/api/greenhouse-observer/assistant/history",
                "/api/greenhouse-observer/assistant/trace",
                "/api/greenhouse-observer/assistant/learning",
                "/api/greenhouse-observer/assistant/queue",
                "/api/greenhouse-observer/assistant/jobs",
            }:
                _json_response(self, 404, {"ok": False, "message": "not found"})
                return
            try:
                query = parse_qs(parsed.query)
                if parsed.path == "/api/greenhouse-observer/history":
                    url = str((query.get("url") or [""])[0]).strip()
                    limit = _coerce_limit(
                        str((query.get("limit") or [""])[0]).strip() or None,
                        default=DEFAULT_HISTORY_LIMIT,
                        maximum=MAX_HISTORY_LIMIT,
                    )
                    payload = recent_manual_sessions_payload(
                        url,
                        session_file=session_file,
                        limit=limit,
                    )
                elif parsed.path == "/api/greenhouse-observer/session":
                    session_id = str((query.get("manual_session_id") or [""])[0]).strip()
                    event_limit = _coerce_limit(
                        str((query.get("event_limit") or [""])[0]).strip() or None,
                        default=DEFAULT_EVENT_LIMIT,
                        maximum=MAX_EVENT_LIMIT,
                    )
                    payload = manual_session_detail_payload(
                        session_id,
                        session_file=session_file,
                        field_events_file=field_events_file,
                        event_limit=event_limit,
                    )
                elif parsed.path == "/api/greenhouse-observer/assistant/run":
                    run_id = str((query.get("run_id") or [""])[0]).strip()
                    payload = assistant_runs.get_run(run_id)
                elif parsed.path == "/api/greenhouse-observer/assistant/runs":
                    payload = assistant_runs.list_runs()
                elif parsed.path == "/api/greenhouse-observer/assistant/history":
                    url = str((query.get("url") or [""])[0]).strip()
                    limit = _coerce_limit(
                        str((query.get("limit") or [""])[0]).strip() or None,
                        default=DEFAULT_ASSISTANT_HISTORY_LIMIT,
                        maximum=MAX_ASSISTANT_HISTORY_LIMIT,
                    )
                    payload = recent_assistant_runs_payload(
                        url,
                        trace_file=trace_file,
                        limit=limit,
                    )
                elif parsed.path == "/api/greenhouse-observer/assistant/learning":
                    url = str((query.get("url") or [""])[0]).strip()
                    limit = _coerce_limit(
                        str((query.get("limit") or [""])[0]).strip() or None,
                        default=DEFAULT_LEARNING_LIMIT,
                        maximum=MAX_LEARNING_LIMIT,
                    )
                    payload = greenhouse_learning_summary_payload(
                        session_file=session_file,
                        field_events_file=field_events_file,
                        requested_url=url or None,
                        limit=limit,
                    )
                elif parsed.path == "/api/greenhouse-observer/assistant/config":
                    profile = _load_json_object_file(profile_file)
                    payload = _assistant_config_payload(profile)
                elif parsed.path == "/api/greenhouse-observer/assistant/queue":
                    payload = assistant_queue.snapshot()
                elif parsed.path == "/api/greenhouse-observer/assistant/jobs":
                    mode = str((query.get("mode") or ["latest_batch"])[0]).strip() or "latest_batch"
                    limit = _coerce_limit(
                        str((query.get("limit") or [""])[0]).strip() or None,
                        default=DEFAULT_JOBS_BATCH_LIMIT,
                        maximum=100,
                    )
                    skip_applied = str((query.get("skip_applied") or ["true"])[0]).strip().lower() not in {
                        "0",
                        "false",
                        "no",
                        "off",
                    }
                    payload = assistant_queue.jobs_batch_payload(
                        mode=mode,
                        limit=limit,
                        skip_applied=skip_applied,
                    )
                elif parsed.path == "/api/greenhouse-observer/assistant/trace":
                    trace_id = str((query.get("trace_id") or [""])[0]).strip()
                    payload = assistant_trace_detail_payload(trace_id, trace_file=trace_file)
                else:
                    url = str((query.get("url") or [""])[0]).strip()
                    if not url:
                        raise CrawlFetchError("Missing url query parameter.")
                    profile = _load_json_object_file(profile_file)
                    approved_answers = _load_json_object_file(answers_file) if answers_file else None
                    payload = _recommendation_payload(
                        url,
                        profile=profile,
                        approved_answers=approved_answers,
                        timeout_seconds=timeout_seconds,
                    )
                _json_response(self, 200, payload)
            except CrawlFetchError as exc:
                _json_response(self, 400, {"ok": False, "message": str(exc)})
            except Exception as exc:  # noqa: BLE001
                _json_response(self, 500, {"ok": False, "message": str(exc)})

        def do_POST(self) -> None:  # noqa: N802
            parsed = urlparse(self.path)
            if parsed.path not in {
                "/api/greenhouse-observer/ingest",
                "/api/greenhouse-observer/assistant/start",
                "/api/greenhouse-observer/assistant/queue",
                "/api/greenhouse-observer/assistant/queue/resume",
                "/api/greenhouse-observer/assistant/queue/batch",
                "/api/greenhouse-observer/assistant/queue/start-next",
            }:
                _json_response(self, 404, {"ok": False, "message": "not found"})
                return
            try:
                length = int(self.headers.get("Content-Length") or "0")
                payload = json.loads(self.rfile.read(length) or b"{}")
                if not isinstance(payload, dict):
                    raise CrawlFetchError("JSON payload must be an object.")
                if parsed.path == "/api/greenhouse-observer/assistant/start":
                    result = assistant_runs.start_run(
                        url=str(payload.get("url") or "").strip(),
                        allow_submit=bool(payload.get("allow_submit")),
                        headless=bool(payload.get("headless")),
                        resume_variant=str(payload.get("resume_variant") or "").strip() or None,
                        resume_path=str(payload.get("resume_path") or "").strip() or None,
                        resume_selection_source=str(payload.get("resume_selection_source") or "").strip() or None,
                    )
                elif parsed.path == "/api/greenhouse-observer/assistant/queue":
                    profile = _load_json_object_file(profile_file)
                    approved_answers = _load_json_object_file(answers_file) if answers_file else None
                    try:
                        resume_selection = _queue_resume_selection_payload(
                            str(payload.get("url") or "").strip(),
                            profile=profile,
                            approved_answers=approved_answers,
                            timeout_seconds=timeout_seconds,
                        )
                    except Exception:
                        resume_selection = {}
                    result = assistant_queue.enqueue_url(
                        requested_url=str(payload.get("url") or "").strip(),
                        company_name=str(payload.get("company_name") or "").strip() or None,
                        job_title=str(payload.get("job_title") or "").strip() or None,
                        fingerprint=str(payload.get("fingerprint") or "").strip() or None,
                        first_seen=str(payload.get("first_seen") or "").strip() or None,
                        source=str(payload.get("source") or "").strip() or None,
                        application_status=str(payload.get("application_status") or "").strip() or None,
                        assistant_last_outcome=str(payload.get("assistant_last_outcome") or "").strip() or None,
                        assistant_last_source=str(payload.get("assistant_last_source") or "").strip() or None,
                        assistant_last_review_pending_count=int(payload.get("assistant_last_review_pending_count") or 0),
                        added_by=str(payload.get("added_by") or "jobs_ui").strip() or "jobs_ui",
                        skip_applied=bool(payload.get("skip_applied", True)),
                        selected_resume_variant=str(
                            payload.get("selected_resume_variant") or resume_selection.get("selected_resume_variant") or ""
                        ).strip()
                        or None,
                        selected_resume_path=str(
                            payload.get("selected_resume_path") or resume_selection.get("selected_resume_path") or ""
                        ).strip()
                        or None,
                        resume_selection_source=str(
                            payload.get("resume_selection_source") or resume_selection.get("resume_selection_source") or ""
                        ).strip()
                        or None,
                    )
                elif parsed.path == "/api/greenhouse-observer/assistant/queue/resume":
                    result = assistant_queue.update_resume_selection(
                        queue_id=str(payload.get("queue_id") or "").strip() or None,
                        requested_url=str(payload.get("url") or "").strip() or None,
                        selected_resume_variant=str(payload.get("selected_resume_variant") or "").strip() or None,
                        selected_resume_path=str(payload.get("selected_resume_path") or "").strip() or None,
                        resume_selection_source=str(payload.get("resume_selection_source") or "").strip() or None,
                    )
                elif parsed.path == "/api/greenhouse-observer/assistant/queue/batch":
                    profile = _load_json_object_file(profile_file)
                    approved_answers = _load_json_object_file(answers_file) if answers_file else None
                    batch_preview = assistant_queue.jobs_batch_payload(
                        mode=str(payload.get("mode") or "latest_batch").strip() or "latest_batch",
                        limit=int(payload.get("limit") or DEFAULT_JOBS_BATCH_LIMIT),
                        skip_applied=bool(payload.get("skip_applied", True)),
                    )
                    selection_by_application_key: dict[str, dict[str, Any]] = {}
                    for candidate in batch_preview.get("candidates") or []:
                        requested_url = str(candidate.get("requested_url") or "").strip()
                        application_key = str(candidate.get("application_key") or "").strip()
                        if not requested_url or not application_key:
                            continue
                        try:
                            selection_by_application_key[application_key] = _queue_resume_selection_payload(
                                requested_url,
                                profile=profile,
                                approved_answers=approved_answers,
                                timeout_seconds=timeout_seconds,
                            )
                        except Exception:
                            continue
                    result = assistant_queue.enqueue_latest_jobs(
                        mode=str(payload.get("mode") or "latest_batch").strip() or "latest_batch",
                        limit=int(payload.get("limit") or DEFAULT_JOBS_BATCH_LIMIT),
                        skip_applied=bool(payload.get("skip_applied", True)),
                        added_by=str(payload.get("added_by") or "assistant_page_batch").strip() or "assistant_page_batch",
                        selection_by_application_key=selection_by_application_key,
                    )
                elif parsed.path == "/api/greenhouse-observer/assistant/queue/start-next":
                    result = assistant_runs.start_next_queued_run(
                        allow_submit=bool(payload.get("allow_submit")),
                        headless=bool(payload.get("headless")),
                    )
                else:
                    profile = _load_json_object_file(profile_file)
                    approved_answers = _load_json_object_file(answers_file) if answers_file else None
                    result = ingest_extension_observation(
                        payload,
                        profile=profile,
                        approved_answers=approved_answers,
                        timeout_seconds=timeout_seconds,
                        session_file=session_file,
                        field_events_file=field_events_file,
                        sync_jobs_db=sync_jobs_db,
                        jobs_db_file=jobs_db_file,
                    )
                _json_response(self, 200, result)
            except CrawlFetchError as exc:
                _json_response(self, 400, {"ok": False, "message": str(exc)})
            except Exception as exc:  # noqa: BLE001
                _json_response(self, 500, {"ok": False, "message": str(exc)})

        def log_message(self, format: str, *args: Any) -> None:  # noqa: A003
            return

    return ObserverHandler


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Serve a localhost API for the Greenhouse observation browser extension."
    )
    parser.add_argument("--host", default=DEFAULT_OBSERVER_API_HOST)
    parser.add_argument("--port", type=int, default=DEFAULT_OBSERVER_API_PORT)
    parser.add_argument("--profile-file", help="JSON profile file used for assistant recommendations.")
    parser.add_argument("--answers-file", help="Optional JSON answers/policy file.")
    parser.add_argument(
        "--timeout-seconds",
        type=int,
        default=DEFAULT_TIMEOUT_SECONDS,
        help=f"Timeout used for Greenhouse schema fetches. Default: {DEFAULT_TIMEOUT_SECONDS}.",
    )
    parser.add_argument(
        "--session-file",
        default=DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
        help="Append-only JSONL manual session output.",
    )
    parser.add_argument(
        "--field-events-file",
        default=DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE,
        help="Append-only JSONL field-event output.",
    )
    parser.add_argument(
        "--trace-file",
        default=DEFAULT_GREENHOUSE_TRACE_FILE,
        help="Append-only JSONL automated assistant run traces.",
    )
    parser.add_argument(
        "--training-examples-file",
        default=DEFAULT_GREENHOUSE_TRAINING_EXAMPLES_FILE,
        help="Append-only JSONL assistant training examples.",
    )
    parser.add_argument(
        "--storage-state-file",
        default=DEFAULT_GREENHOUSE_STORAGE_STATE_FILE,
        help="Path to persisted Playwright browser storage used to reuse login state across assistant runs.",
    )
    parser.add_argument(
        "--queue-file",
        default=DEFAULT_GREENHOUSE_ASSISTANT_QUEUE_FILE,
        help="JSON assistant task queue for scraped Greenhouse jobs.",
    )
    parser.add_argument(
        "--sync-jobs-db",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Sync observed Greenhouse activity back into jobs.db.",
    )
    parser.add_argument(
        "--jobs-db-file",
        default=DEFAULT_JOBS_DB_FILE,
        help="Path to jobs.db used for syncing observer activity.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    handler = make_observer_handler(
        profile_file=args.profile_file,
        answers_file=args.answers_file,
        timeout_seconds=args.timeout_seconds,
        session_file=args.session_file,
        field_events_file=args.field_events_file,
        trace_file=args.trace_file,
        training_examples_file=args.training_examples_file,
        storage_state_file=args.storage_state_file,
        queue_file=args.queue_file,
        sync_jobs_db=bool(args.sync_jobs_db),
        jobs_db_file=args.jobs_db_file,
    )
    server = ThreadingHTTPServer((args.host, args.port), handler)
    print(
        json.dumps(
            {
                "ok": True,
                "host": args.host,
                "port": args.port,
                "health_url": f"http://{args.host}:{args.port}/api/health",
                "recommendation_url": f"http://{args.host}:{args.port}/api/greenhouse-observer/recommendation",
                "ingest_url": f"http://{args.host}:{args.port}/api/greenhouse-observer/ingest",
                "assistant_start_url": f"http://{args.host}:{args.port}/api/greenhouse-observer/assistant/start",
                "assistant_run_url": f"http://{args.host}:{args.port}/api/greenhouse-observer/assistant/run",
                "assistant_runs_url": f"http://{args.host}:{args.port}/api/greenhouse-observer/assistant/runs",
                "assistant_history_url": f"http://{args.host}:{args.port}/api/greenhouse-observer/assistant/history",
                "assistant_queue_url": f"http://{args.host}:{args.port}/api/greenhouse-observer/assistant/queue",
                "assistant_jobs_url": f"http://{args.host}:{args.port}/api/greenhouse-observer/assistant/jobs",
            },
            ensure_ascii=False,
        )
    )
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
