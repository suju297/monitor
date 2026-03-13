from __future__ import annotations

import argparse
import json
import sys
import time
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

from .constants import DEFAULT_TIMEOUT_SECONDS
from .exceptions import CrawlFetchError
from .greenhouse_assistant import (
    GreenhouseApplicationAnalysis,
    build_approved_answer_targets,
    build_autofill_targets,
    inspect_greenhouse_application,
)
from .greenhouse_page_observer import (
    drain_greenhouse_page_observer,
    install_greenhouse_page_observer,
)
from .jobs_db_sync import DEFAULT_JOBS_DB_FILE, sync_greenhouse_job_record
from .greenhouse_playwright import (
    _browser_launch_kwargs,
    _ensure_form_visible,
    _evaluate_confirmation_signals,
)
from .greenhouse_submission_state import build_greenhouse_application_key
from .greenhouse_trace_store import (
    DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE,
    DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
    append_greenhouse_manual_observation,
)


@dataclass
class ManualObservationResult:
    analysis: dict[str, Any]
    page_url: str
    form_visible: bool
    event_count: int
    confirmation_detected: bool
    ended_reason: str | None = None
    trace: dict[str, Any] | None = None
    jobs_db_sync: dict[str, Any] | None = None
    errors: list[str] | None = None

    def to_dict(self) -> dict[str, Any]:
        payload = asdict(self)
        payload["errors"] = list(self.errors or [])
        return payload


def _load_json_object_file(path: str | None, *, required_label: str) -> dict[str, Any]:
    if not path:
        return {}
    payload = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise CrawlFetchError(f"{required_label.capitalize()} file must contain a JSON object.")
    return payload


def _manual_confirmation_state(page, *, previous_url: str) -> tuple[bool, str | None]:
    try:
        title = page.title()
    except Exception:
        title = ""
    try:
        page_text = " ".join((page.locator("body").inner_text(timeout=500) or "").split())[:3000]
    except Exception:
        page_text = ""
    try:
        form_visible = page.locator("form#application-form, .application--form").first.is_visible(timeout=300)
    except Exception:
        form_visible = False
    return _evaluate_confirmation_signals(
        current_url=page.url,
        previous_url=previous_url,
        title=title,
        page_text=page_text,
        form_visible=form_visible,
    )


def observe_greenhouse_manual_session(
    url: str,
    *,
    profile: dict[str, Any] | None = None,
    approved_answers: dict[str, Any] | None = None,
    headless: bool = False,
    timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS,
    poll_interval_seconds: float = 1.0,
    auto_stop_on_confirmation: bool = True,
    store_traces: bool = True,
    session_file: str = DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
    field_events_file: str = DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE,
    resume_decision_source: str | None = None,
    external_resume_recommendation: str | None = None,
    sync_jobs_db: bool = True,
    jobs_db_file: str = DEFAULT_JOBS_DB_FILE,
) -> ManualObservationResult:
    try:
        from playwright.sync_api import sync_playwright
    except Exception as exc:  # noqa: BLE001
        raise CrawlFetchError(
            "playwright is not installed. Install with: uv sync --extra playwright && uv run playwright install chromium"
        ) from exc

    prepared_profile = profile or {}
    analysis: GreenhouseApplicationAnalysis = inspect_greenhouse_application(
        url,
        profile=prepared_profile,
        approved_answers=approved_answers,
        timeout_seconds=timeout_seconds,
    )
    fill_targets = build_autofill_targets(analysis, profile=prepared_profile)
    fill_targets.extend(
        build_approved_answer_targets(
            analysis,
            approved_answers=approved_answers,
            profile=prepared_profile,
        )
    )
    application_key = build_greenhouse_application_key(
        board_token=analysis.schema.detection.board_token,
        job_id=analysis.schema.detection.job_id,
        public_url=analysis.schema.public_url or url,
    )

    timeout_ms = max(1000, timeout_seconds * 1000)
    observed_events: list[dict[str, Any]] = []
    last_page_url = url
    form_visible = False
    confirmation_detected = False
    ended_reason: str | None = None
    errors: list[str] = []

    with sync_playwright() as pw:
        browser = pw.chromium.launch(**_browser_launch_kwargs(headless=headless))
        page = browser.new_page()
        install_greenhouse_page_observer(page)
        try:
            page.goto(url, wait_until="domcontentloaded", timeout=timeout_ms)
            try:
                page.wait_for_load_state("networkidle", timeout=min(timeout_ms, 8000))
            except Exception:
                pass
            form_visible = _ensure_form_visible(page, timeout_ms)
            last_page_url = page.url

            while True:
                observed_events.extend(drain_greenhouse_page_observer(page))
                try:
                    last_page_url = page.url
                except Exception:
                    pass
                if auto_stop_on_confirmation:
                    confirmed, note = _manual_confirmation_state(
                        page,
                        previous_url=analysis.schema.public_url or url,
                    )
                    if confirmed:
                        confirmation_detected = True
                        ended_reason = note or "confirmation_detected"
                        break
                if page.is_closed():
                    ended_reason = "page_closed"
                    break
                time.sleep(max(0.2, poll_interval_seconds))
        except KeyboardInterrupt:
            ended_reason = "keyboard_interrupt"
        except Exception as exc:  # noqa: BLE001
            errors.append(str(exc))
            ended_reason = "observer_error"
        finally:
            observed_events.extend(drain_greenhouse_page_observer(page))
            try:
                if not page.is_closed():
                    last_page_url = page.url
            except Exception:
                pass
            try:
                browser.close()
            except Exception:
                pass

    result = ManualObservationResult(
        analysis=analysis.to_dict(),
        page_url=last_page_url,
        form_visible=form_visible,
        event_count=len(observed_events),
        confirmation_detected=confirmation_detected,
        ended_reason=ended_reason,
        errors=errors,
    )
    if store_traces:
        try:
            result.trace = append_greenhouse_manual_observation(
                analysis=analysis,
                observed_events=observed_events,
                profile=prepared_profile,
                fill_targets=fill_targets,
                application_key=application_key,
                requested_url=url,
                final_page_url=last_page_url,
                confirmation_detected=confirmation_detected,
                ended_reason=ended_reason,
                resume_decision_source=resume_decision_source,
                external_resume_recommendation=external_resume_recommendation,
                session_file=session_file,
                field_events_file=field_events_file,
            )
        except Exception as exc:  # noqa: BLE001
            result.errors = list(result.errors or []) + [f"Manual trace persistence failed: {exc}"]
    try:
        pending_review_count = sum(
            1 for item in analysis.review_queue if item.status in {"pending", "blocked"}
        )
        result.jobs_db_sync = sync_greenhouse_job_record(
            requested_url=url,
            public_url=analysis.schema.public_url,
            source="manual_observer",
            outcome="submitted" if confirmation_detected else "observed",
            application_status="applied" if confirmation_detected else None,
            manual_session_id=str((result.trace or {}).get("manual_session_id") or "").strip() or None,
            auto_submit_eligible=bool(analysis.auto_submit_eligible),
            review_pending_count=pending_review_count,
            confirmation_detected=confirmation_detected,
            jobs_db_file=jobs_db_file,
            enabled=sync_jobs_db,
        )
    except Exception as exc:  # noqa: BLE001
        result.errors = list(result.errors or []) + [f"jobs.db sync failed: {exc}"]
        result.jobs_db_sync = {"stored": False, "reason": f"sync_failed: {exc}"}
    return result


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Observe a manual Greenhouse application session and persist learning traces."
    )
    parser.add_argument("url", help="Greenhouse hosted job URL.")
    parser.add_argument("--profile-file", help="Optional JSON profile file for recommendation comparison.")
    parser.add_argument(
        "--answers-file",
        help="Optional JSON file with approved answers or policy answers for recommendation comparison.",
    )
    parser.add_argument(
        "--timeout-seconds",
        type=int,
        default=DEFAULT_TIMEOUT_SECONDS,
        help=f"Timeout for page operations. Default: {DEFAULT_TIMEOUT_SECONDS}.",
    )
    parser.add_argument(
        "--headless",
        action=argparse.BooleanOptionalAction,
        default=False,
        help="Run Chromium headless. Manual observation is usually better with --no-headless.",
    )
    parser.add_argument(
        "--poll-interval-seconds",
        type=float,
        default=1.0,
        help="How often to drain browser events while you fill the form.",
    )
    parser.add_argument(
        "--auto-stop-on-confirmation",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Stop automatically when a confirmation page or thank-you state is detected.",
    )
    parser.add_argument(
        "--store-traces",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Persist manual session and field-event traces.",
    )
    parser.add_argument(
        "--session-file",
        default=DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
        help="Path to append-only JSONL manual session records.",
    )
    parser.add_argument(
        "--field-events-file",
        default=DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE,
        help="Path to append-only JSONL manual field-event records.",
    )
    parser.add_argument(
        "--sync-jobs-db",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Sync observed application activity back into jobs.db.",
    )
    parser.add_argument(
        "--jobs-db-file",
        default=DEFAULT_JOBS_DB_FILE,
        help="Path to jobs.db for syncing manual observation outcomes.",
    )
    parser.add_argument(
        "--resume-decision-source",
        help="Optional label for how you picked the resume: chatgpt, manual, assistant, mixed, etc.",
    )
    parser.add_argument(
        "--external-resume-recommendation",
        help="Optional external recommendation to compare against the final resume choice.",
    )
    parser.add_argument(
        "--indent",
        type=int,
        default=2,
        help="JSON indentation level for output.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        profile = _load_json_object_file(args.profile_file, required_label="profile")
        approved_answers = _load_json_object_file(args.answers_file, required_label="approved answers")
        result = observe_greenhouse_manual_session(
            args.url,
            profile=profile,
            approved_answers=approved_answers,
            headless=bool(args.headless),
            timeout_seconds=args.timeout_seconds,
            poll_interval_seconds=args.poll_interval_seconds,
            auto_stop_on_confirmation=bool(args.auto_stop_on_confirmation),
            store_traces=bool(args.store_traces),
            session_file=args.session_file,
            field_events_file=args.field_events_file,
            resume_decision_source=args.resume_decision_source,
            external_resume_recommendation=args.external_resume_recommendation,
            sync_jobs_db=bool(args.sync_jobs_db),
            jobs_db_file=args.jobs_db_file,
        )
    except Exception as exc:  # noqa: BLE001
        print(f"error: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result.to_dict(), indent=args.indent, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
