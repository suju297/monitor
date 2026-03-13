from __future__ import annotations

import hashlib
import json
from pathlib import Path
from typing import Any

from .greenhouse_assistant import GreenhouseApplicationAnalysis, GreenhouseFillTarget, GreenhouseQuestion
from .utils import utc_now

DEFAULT_GREENHOUSE_TRACE_FILE = ".state/greenhouse_run_traces.jsonl"
DEFAULT_GREENHOUSE_TRAINING_EXAMPLES_FILE = ".state/greenhouse_training_examples.jsonl"
DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE = ".state/greenhouse_manual_sessions.jsonl"
DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE = ".state/greenhouse_manual_field_events.jsonl"
TRACE_SCHEMA_VERSION = 1

_PROFILE_POLICY_KEYS = (
    "authorized_to_work_in_us",
    "requires_visa_sponsorship_now",
    "requires_visa_sponsorship_future",
    "requires_sponsorship_if_opt_included",
    "current_visa_status",
    "current_country",
    "current_state_or_region",
    "open_to_relocation",
    "default_remote_work_answer",
    "whatsapp_opt_in_default",
    "default_authorized_to_work_answer",
    "default_requires_sponsorship_answer",
)
_SENSITIVE_PROFILE_KEYS = {
    "first_name",
    "preferred_first_name",
    "preferred_name",
    "last_name",
    "full_name",
    "email",
    "phone",
    "location",
    "linkedin",
    "github",
    "website",
    "resume_path",
    "cover_letter_path",
}
_SENSITIVE_FIELD_HINTS = {
    "first name": "first_name",
    "preferred first name": "preferred_first_name",
    "preferred name": "preferred_name",
    "last name": "last_name",
    "full name": "full_name",
    "email": "email",
    "email address": "email",
    "phone": "phone",
    "phone number": "phone",
    "linkedin": "linkedin",
    "linkedin profile": "linkedin",
    "github": "github",
    "website": "website",
    "portfolio": "website",
    "website or portfolio": "website",
    "location": "location",
    "resume": "resume_path",
    "resume cv": "resume_path",
    "cover letter": "cover_letter_path",
}


def _normalized_text(value: str | None) -> str:
    raw = (value or "").strip().lower()
    normalized = "".join(ch if ch.isalnum() else " " for ch in raw)
    return " ".join(normalized.split())


def _absolute_path(path: str | Path) -> Path:
    value = Path(path).expanduser()
    if not value.is_absolute():
        value = Path.cwd() / value
    return value


def _has_value(value: Any) -> bool:
    if isinstance(value, bool):
        return True
    if value is None:
        return False
    if isinstance(value, str):
        return bool(value.strip())
    if isinstance(value, (list, tuple, set, dict)):
        return bool(value)
    return True


def _truncate(value: str, limit: int) -> str:
    text = " ".join(value.split())
    if len(text) <= limit:
        return text
    return text[: limit - 3].rstrip() + "..."


def _stable_hash(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()[:16]


def _candidate_id(profile: dict[str, Any] | None) -> str | None:
    if not isinstance(profile, dict):
        return None
    parts: list[str] = []
    for key in ("email", "phone", "linkedin", "github", "full_name", "first_name", "last_name"):
        raw = profile.get(key)
        if not _has_value(raw):
            continue
        parts.append(f"{key}={str(raw).strip().lower()}")
    if not parts:
        return None
    return f"cand_{_stable_hash('|'.join(parts))}"


def _profile_snapshot(
    profile: dict[str, Any] | None,
    fill_targets: list[GreenhouseFillTarget],
) -> dict[str, Any]:
    if not isinstance(profile, dict):
        return {}
    snapshot: dict[str, Any] = {}
    candidate_id = _candidate_id(profile)
    if candidate_id:
        snapshot["candidate_id"] = candidate_id

    provided_keys = sorted(str(key) for key, value in profile.items() if _has_value(value))
    if provided_keys:
        snapshot["provided_keys"] = provided_keys

    email = str(profile.get("email") or "").strip().lower()
    if "@" in email:
        snapshot["email_domain"] = email.split("@", 1)[1]

    policy_fields = {
        key: profile.get(key)
        for key in _PROFILE_POLICY_KEYS
        if key in profile and _has_value(profile.get(key))
    }
    if policy_fields:
        snapshot["policy_fields"] = policy_fields

    resume_variants = profile.get("resume_variants")
    if isinstance(resume_variants, dict):
        snapshot["resume_variant_keys"] = sorted(str(key) for key in resume_variants)
    if _has_value(profile.get("resume_variant_default")):
        snapshot["resume_variant_default"] = str(profile.get("resume_variant_default")).strip()
    if _has_value(profile.get("selected_resume_variant")):
        snapshot["selected_resume_variant"] = str(profile.get("selected_resume_variant")).strip()
    if _has_value(profile.get("selected_resume_path")):
        snapshot["selected_resume_path"] = str(profile.get("selected_resume_path")).strip()
    if _has_value(profile.get("resume_selection_source")):
        snapshot["resume_selection_source"] = str(profile.get("resume_selection_source")).strip()

    for target in fill_targets:
        if target.profile_key == "resume_path" and "selected_resume_file" not in snapshot:
            snapshot["selected_resume_file"] = Path(target.value).name
        if target.profile_key == "cover_letter_path" and "selected_cover_letter_file" not in snapshot:
            snapshot["selected_cover_letter_file"] = Path(target.value).name
    return snapshot


def _answer_keys(approved_answers: dict[str, Any] | None) -> list[str]:
    if not isinstance(approved_answers, dict):
        return []
    return sorted(str(key) for key, value in approved_answers.items() if _has_value(value))


def _count(values: list[str]) -> dict[str, int]:
    counts: dict[str, int] = {}
    for value in values:
        if not value:
            continue
        counts[value] = counts.get(value, 0) + 1
    return counts


def _coerce_string_list(value: Any) -> list[str]:
    if not isinstance(value, (list, tuple, set)):
        return []
    return [str(item).strip() for item in value if str(item).strip()]


def _append_jsonl(path: Path, records: list[dict[str, Any]]) -> None:
    if not records:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        for record in records:
            handle.write(json.dumps(record, sort_keys=True, ensure_ascii=False))
            handle.write("\n")


def _question_key(question_label: str, api_name: str | None) -> str:
    return f"{_normalized_text(question_label)}::{_normalized_text(api_name)}"


def _build_target_maps(
    fill_targets: list[GreenhouseFillTarget],
) -> tuple[dict[str, GreenhouseFillTarget], dict[str, GreenhouseFillTarget]]:
    by_question: dict[str, GreenhouseFillTarget] = {}
    by_label: dict[str, GreenhouseFillTarget] = {}
    for target in fill_targets:
        by_question.setdefault(_question_key(target.question_label, target.api_name), target)
        by_label.setdefault(_normalized_text(target.question_label), target)
    return by_question, by_label


def _build_execution_maps(
    result: dict[str, Any],
) -> tuple[dict[str, dict[str, Any]], dict[str, dict[str, Any]]]:
    by_question: dict[str, dict[str, Any]] = {}
    by_label: dict[str, dict[str, Any]] = {}
    for field_name in ("filled", "skipped"):
        for item in result.get(field_name) or []:
            if not isinstance(item, dict):
                continue
            question_label = str(item.get("question_label") or "").strip()
            api_name = str(item.get("api_name") or "").strip()
            key = _question_key(question_label, api_name)
            by_question.setdefault(key, item)
            by_label.setdefault(_normalized_text(question_label), item)
    return by_question, by_label


def _build_review_maps(
    analysis: GreenhouseApplicationAnalysis,
) -> tuple[dict[str, dict[str, Any]], dict[str, dict[str, Any]]]:
    by_question: dict[str, dict[str, Any]] = {}
    by_label: dict[str, dict[str, Any]] = {}
    for item in analysis.review_queue:
        payload = {
            "question_label": item.question_label,
            "api_name": item.api_name,
            "field_type": item.field_type,
            "required": item.required,
            "group": item.group,
            "decision": item.decision,
            "reason": item.reason,
            "approved_answer": item.approved_answer,
            "status": item.status,
        }
        by_question.setdefault(_question_key(item.question_label, item.api_name), payload)
        by_label.setdefault(_normalized_text(item.question_label), payload)
    return by_question, by_label


def _build_suggested_maps(
    analysis: GreenhouseApplicationAnalysis,
) -> tuple[dict[str, dict[str, Any]], dict[str, dict[str, Any]]]:
    by_question: dict[str, dict[str, Any]] = {}
    by_label: dict[str, dict[str, Any]] = {}
    for item in analysis.suggested_answers:
        payload = {
            "question_label": item.question_label,
            "api_name": item.api_name,
            "source": item.source,
            "reason": item.reason,
            "confidence": item.confidence,
            "draft_source": getattr(item, "draft_source", None),
            "retrieved_chunk_ids": _coerce_string_list(getattr(item, "retrieved_chunk_ids", [])),
            "style_snippet_ids": _coerce_string_list(getattr(item, "style_snippet_ids", [])),
            "retrieval_summary": _coerce_string_list(getattr(item, "retrieval_summary", [])),
        }
        by_question.setdefault(_question_key(item.question_label, item.api_name), payload)
        by_label.setdefault(_normalized_text(item.question_label), payload)
    return by_question, by_label


def _preview_target_value(target: GreenhouseFillTarget | None) -> str | None:
    if target is None:
        return None
    if target.value_source == "profile":
        if target.profile_key == "email":
            email = target.value.strip()
            return email[:3] + "***" if len(email) > 3 else "***"
        if target.profile_key in {"resume_path", "cover_letter_path"}:
            return Path(target.value).name
        return _truncate(target.value, 60)
    return _truncate(target.value, 160)


def _resolved_answer(target: GreenhouseFillTarget | None) -> str | None:
    if target is None or target.value_source == "profile":
        return None
    return target.value[:4000]


def _option_labels(question: GreenhouseQuestion) -> list[str]:
    labels: list[str] = []
    for input_field in question.inputs:
        for option in input_field.options:
            label = option.label or option.value
            if label:
                labels.append(label)
    return labels[:40]


def _resume_variant_map(profile: dict[str, Any] | None) -> dict[str, str]:
    if not isinstance(profile, dict):
        return {}
    raw = profile.get("resume_variants")
    if not isinstance(raw, dict):
        return {}
    variants: dict[str, str] = {}
    for key, value in raw.items():
        if isinstance(value, dict):
            path = str(value.get("path") or "").strip()
        else:
            path = str(value or "").strip()
        if path:
            variants[str(key).strip()] = str(Path(path).expanduser())
    return variants


def _match_resume_variant(profile: dict[str, Any] | None, filename: str | None) -> str | None:
    if not filename:
        return None
    candidate = str(filename).strip()
    candidate_path = str(Path(candidate).expanduser())
    normalized = Path(candidate).name.strip().lower()
    basename_matches: list[str] = []
    for variant, stored_path in _resume_variant_map(profile).items():
        if stored_path == candidate_path:
            return variant
        if Path(stored_path).name.strip().lower() == normalized:
            basename_matches.append(variant)
    if len(basename_matches) == 1:
        return basename_matches[0]
    return None


def _question_maps(
    analysis: GreenhouseApplicationAnalysis,
) -> tuple[dict[str, GreenhouseQuestion], dict[str, GreenhouseQuestion]]:
    by_question: dict[str, GreenhouseQuestion] = {}
    by_label: dict[str, GreenhouseQuestion] = {}
    for question in analysis.schema.questions:
        primary_input = question.inputs[0] if question.inputs else None
        key = _question_key(question.label, primary_input.api_name if primary_input else None)
        by_question.setdefault(key, question)
        by_label.setdefault(_normalized_text(question.label), question)
    return by_question, by_label


def _recommended_target_maps(
    fill_targets: list[GreenhouseFillTarget],
) -> tuple[dict[str, GreenhouseFillTarget], dict[str, GreenhouseFillTarget]]:
    return _build_target_maps(fill_targets)


def _manual_event_value(
    *,
    value: str | None,
    file_names: list[str] | None,
) -> str:
    if file_names:
        return ", ".join(name for name in file_names if name)
    return str(value or "")


def _sensitive_preview(profile_key: str | None, value: str) -> str | None:
    key = str(profile_key or "").strip()
    raw = value.strip()
    if not raw:
        return None
    if key == "email":
        return raw[:3] + "***" if len(raw) > 3 else "***"
    if key in {"resume_path", "cover_letter_path"}:
        return Path(raw).name
    if len(raw) <= 3:
        return "*" * len(raw)
    return raw[:1] + "***"


def _manual_value_payload(
    *,
    raw_value: str,
    question: GreenhouseQuestion | None,
    recommended_target: GreenhouseFillTarget | None,
    event_question_label: str | None = None,
    event_api_name: str | None = None,
    file_names: list[str] | None = None,
) -> dict[str, Any]:
    profile_key = recommended_target.profile_key if recommended_target else None
    if not profile_key and question and question.decision:
        profile_key = question.decision.profile_key
    if not profile_key:
        label_key = _normalized_text(event_question_label)
        api_key = _normalized_text(event_api_name)
        profile_key = _SENSITIVE_FIELD_HINTS.get(label_key) or _SENSITIVE_FIELD_HINTS.get(api_key)
    if file_names:
        preview = ", ".join(file_names)[:240]
        return {
            "value_preview": preview or None,
            "stored_value": preview or None,
            "value_hash": _stable_hash(preview.lower()) if preview else None,
        }

    normalized = raw_value.strip()
    if not normalized:
        return {
            "value_preview": None,
            "stored_value": None,
            "value_hash": None,
        }

    if recommended_target and recommended_target.value_source == "profile" and profile_key in _SENSITIVE_PROFILE_KEYS:
        stored = normalized[:4000]
        return {
            "value_preview": _truncate(stored, 160),
            "stored_value": stored,
            "value_hash": _stable_hash(normalized.lower()),
        }

    if profile_key in _SENSITIVE_PROFILE_KEYS:
        stored = normalized[:4000]
        return {
            "value_preview": _truncate(stored, 160),
            "stored_value": stored,
            "value_hash": _stable_hash(normalized.lower()),
        }

    stored = normalized[:4000]
    return {
        "value_preview": _truncate(stored, 160),
        "stored_value": stored,
        "value_hash": _stable_hash(normalized.lower()),
    }


def _final_manual_answers(observed_events: list[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    final_by_question: dict[str, dict[str, Any]] = {}
    for event in observed_events:
        event_type = str(event.get("event_type") or "").strip().lower()
        if event_type not in {"change", "blur"}:
            continue
        question_label = str(event.get("question_label") or "").strip()
        api_name = str(event.get("api_name") or "").strip()
        key = _question_key(question_label, api_name)
        final_by_question[key] = event
    return final_by_question


def build_greenhouse_run_trace(
    *,
    analysis: GreenhouseApplicationAnalysis,
    result: dict[str, Any],
    profile: dict[str, Any] | None,
    approved_answers: dict[str, Any] | None,
    fill_targets: list[GreenhouseFillTarget],
    application_key: str,
    requested_url: str,
    captured_at: str | None = None,
) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    captured_at = captured_at or utc_now()
    trace_seed = "|".join(
        [
            application_key,
            requested_url,
            str(result.get("page_url") or ""),
            captured_at,
        ]
    )
    trace_id = f"ghtrace_{_stable_hash(trace_seed)}"
    result_summary = {
        "form_visible": bool(result.get("form_visible")),
        "filled_count": len(result.get("filled") or []),
        "skipped_count": len(result.get("skipped") or []),
        "error_count": len(result.get("errors") or []),
        "browser_validation_error_count": len(result.get("browser_validation_errors") or []),
        "review_queue_count": len(analysis.review_queue),
        "review_queue_status_counts": _count([item.status for item in analysis.review_queue]),
        "decision_counts": dict(analysis.decision_counts),
        "auto_submit_eligible": bool(analysis.auto_submit_eligible),
        "eligible_after_browser_validation": bool(
            (result.get("submit_safety") or {}).get("eligible_after_browser_validation")
        ),
        "submission_requested": bool((result.get("submission") or {}).get("requested")),
        "submission_attempted": bool((result.get("submission") or {}).get("attempted")),
        "submitted": bool((result.get("submission") or {}).get("submitted")),
        "confirmation_detected": bool((result.get("submission") or {}).get("confirmation_detected")),
    }
    run_trace = {
        "trace_version": TRACE_SCHEMA_VERSION,
        "record_type": "greenhouse_run_trace",
        "trace_id": trace_id,
        "captured_at": captured_at,
        "application_key": application_key,
        "requested_url": requested_url,
        "public_url": analysis.schema.public_url,
        "page_url": result.get("page_url"),
        "company_name": analysis.schema.company_name,
        "job_title": analysis.schema.title,
        "board_token": analysis.schema.detection.board_token,
        "job_id": analysis.schema.detection.job_id,
        "schema_source": analysis.schema.source,
        "candidate": _profile_snapshot(profile, fill_targets),
        "run_resume_selection": result.get("resume_selection") or None,
        "approved_answer_keys": _answer_keys(approved_answers),
        "target_summary": {
            "total": len(fill_targets),
            "value_source_counts": _count([target.value_source for target in fill_targets]),
            "ui_type_counts": _count([target.ui_type for target in fill_targets]),
        },
        "result_summary": result_summary,
        "result": result,
    }
    return run_trace, _build_greenhouse_question_examples(
        analysis=analysis,
        result=result,
        fill_targets=fill_targets,
        trace_id=trace_id,
        captured_at=captured_at,
        application_key=application_key,
    )


def _build_greenhouse_question_examples(
    *,
    analysis: GreenhouseApplicationAnalysis,
    result: dict[str, Any],
    fill_targets: list[GreenhouseFillTarget],
    trace_id: str,
    captured_at: str,
    application_key: str,
) -> list[dict[str, Any]]:
    target_by_question, target_by_label = _build_target_maps(fill_targets)
    execution_by_question, execution_by_label = _build_execution_maps(result)
    review_by_question, review_by_label = _build_review_maps(analysis)
    suggested_by_question, suggested_by_label = _build_suggested_maps(analysis)
    submit_safety = result.get("submit_safety") or {}
    submission = result.get("submission") or {}
    examples: list[dict[str, Any]] = []

    for question in analysis.schema.questions:
        primary_input = question.inputs[0] if question.inputs else None
        question_key = _question_key(question.label, primary_input.api_name if primary_input else None)
        target = target_by_question.get(question_key) or target_by_label.get(_normalized_text(question.label))
        execution = execution_by_question.get(question_key) or execution_by_label.get(_normalized_text(question.label))
        review_item = review_by_question.get(question_key) or review_by_label.get(_normalized_text(question.label))
        suggested_item = suggested_by_question.get(question_key) or suggested_by_label.get(_normalized_text(question.label))
        decision = question.decision

        if execution:
            execution_status = "filled" if execution.get("success") else "skipped"
        elif review_item and review_item.get("status") == "pending":
            execution_status = "pending_review"
        else:
            execution_status = "not_attempted"

        examples.append(
            {
                "trace_version": TRACE_SCHEMA_VERSION,
                "record_type": "greenhouse_question_example",
                "trace_id": trace_id,
                "captured_at": captured_at,
                "application_key": application_key,
                "company_name": analysis.schema.company_name,
                "job_title": analysis.schema.title,
                "board_token": analysis.schema.detection.board_token,
                "job_id": analysis.schema.detection.job_id,
                "public_url": analysis.schema.public_url,
                "page_url": result.get("page_url"),
                "question_label": question.label,
                "normalized_label": _normalized_text(question.label),
                "question_group": question.group,
                "question_description": question.description,
                "required": question.required,
                "api_names": [item.api_name for item in question.inputs if item.api_name],
                "field_types": [item.ui_type for item in question.inputs if item.ui_type],
                "option_labels": _option_labels(question),
                "decision_action": decision.action if decision else None,
                "decision_reason": decision.reason if decision else None,
                "decision_source": decision.source if decision else None,
                "decision_profile_key": decision.profile_key if decision else None,
                "review_status": review_item.get("status") if review_item else None,
                "review_field_type": review_item.get("field_type") if review_item else None,
                "approved_answer_present": bool(review_item and review_item.get("approved_answer")),
                "resolved_answer_source": target.value_source if target else None,
                "resolved_answer": _resolved_answer(target),
                "resolved_answer_preview": _preview_target_value(target),
                "draft_source": suggested_item.get("draft_source") if suggested_item else None,
                "retrieved_chunk_ids": suggested_item.get("retrieved_chunk_ids") if suggested_item else [],
                "style_snippet_ids": suggested_item.get("style_snippet_ids") if suggested_item else [],
                "retrieval_summary": suggested_item.get("retrieval_summary") if suggested_item else [],
                "fill_attempted": execution is not None,
                "fill_success": execution.get("success") if execution else None,
                "fill_error": execution.get("error") if execution else None,
                "fill_bound_via": execution.get("bound_via") if execution else None,
                "fill_verification": execution.get("verification") if execution else None,
                "execution_status": execution_status,
                "auto_submit_eligible": bool(analysis.auto_submit_eligible),
                "eligible_after_browser_validation": bool(
                    submit_safety.get("eligible_after_browser_validation")
                ),
                "submission_requested": bool(submission.get("requested")),
                "submission_attempted": bool(submission.get("attempted")),
                "submitted": bool(submission.get("submitted")),
            }
        )
    return examples


def append_greenhouse_trace(
    *,
    analysis: GreenhouseApplicationAnalysis,
    result: dict[str, Any],
    profile: dict[str, Any] | None,
    approved_answers: dict[str, Any] | None,
    fill_targets: list[GreenhouseFillTarget],
    application_key: str,
    requested_url: str,
    trace_file: str | Path = DEFAULT_GREENHOUSE_TRACE_FILE,
    training_examples_file: str | Path = DEFAULT_GREENHOUSE_TRAINING_EXAMPLES_FILE,
) -> dict[str, Any]:
    trace_path = _absolute_path(trace_file)
    examples_path = _absolute_path(training_examples_file)
    run_trace, question_examples = build_greenhouse_run_trace(
        analysis=analysis,
        result=result,
        profile=profile,
        approved_answers=approved_answers,
        fill_targets=fill_targets,
        application_key=application_key,
        requested_url=requested_url,
    )
    _append_jsonl(trace_path, [run_trace])
    _append_jsonl(examples_path, question_examples)
    return {
        "stored": True,
        "trace_id": run_trace["trace_id"],
        "trace_file": str(trace_path),
        "training_examples_file": str(examples_path),
        "run_record_count": 1,
        "question_record_count": len(question_examples),
    }


def _manual_matches_recommendation(
    event: dict[str, Any],
    target: GreenhouseFillTarget | None,
) -> bool | None:
    if target is None:
        return None
    file_names = event.get("file_names")
    if isinstance(file_names, list) and file_names:
        observed = str(file_names[0]).strip().lower()
        expected = Path(target.value).name.strip().lower()
        return observed == expected
    observed_value = _normalized_text(str(event.get("value") or ""))
    expected_value = _normalized_text(target.value)
    if not observed_value and not expected_value:
        return True
    if not observed_value:
        return False
    return observed_value == expected_value


def build_greenhouse_manual_observation_trace(
    *,
    analysis: GreenhouseApplicationAnalysis,
    observed_events: list[dict[str, Any]],
    profile: dict[str, Any] | None,
    fill_targets: list[GreenhouseFillTarget],
    application_key: str,
    requested_url: str,
    final_page_url: str | None = None,
    confirmation_detected: bool = False,
    ended_reason: str | None = None,
    resume_decision_source: str | None = None,
    external_resume_recommendation: str | None = None,
    captured_at: str | None = None,
) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    captured_at = captured_at or utc_now()
    final_page_url = final_page_url or requested_url
    session_seed = "|".join(
        [application_key, requested_url, final_page_url, captured_at, str(len(observed_events))]
    )
    manual_session_id = f"ghmanual_{_stable_hash(session_seed)}"
    question_by_key, question_by_label = _question_maps(analysis)
    target_by_question, target_by_label = _recommended_target_maps(fill_targets)
    suggested_by_question, suggested_by_label = _build_suggested_maps(analysis)

    event_records: list[dict[str, Any]] = []
    focus_order: list[str] = []
    event_types: list[str] = []
    touched_questions: set[str] = set()
    for event in observed_events:
        event_type = str(event.get("event_type") or "").strip().lower()
        event_types.append(event_type)
        question_label = str(event.get("question_label") or "").strip()
        api_name = str(event.get("api_name") or "").strip()
        label_key = _normalized_text(question_label)
        if label_key:
            touched_questions.add(label_key)
        if event_type == "focus" and question_label:
            focus_order.append(question_label)

        question_key = _question_key(question_label, api_name)
        question = question_by_key.get(question_key) or question_by_label.get(label_key)
        target = target_by_question.get(question_key) or target_by_label.get(label_key)
        suggested = suggested_by_question.get(question_key) or suggested_by_label.get(label_key)
        file_names = event.get("file_names")
        normalized_files = [str(name).strip() for name in file_names] if isinstance(file_names, list) else []
        payload = _manual_value_payload(
            raw_value=_manual_event_value(value=str(event.get("value") or ""), file_names=normalized_files),
            question=question,
            recommended_target=target,
            event_question_label=question_label,
            event_api_name=api_name,
            file_names=normalized_files,
        )
        matched_recommendation = _manual_matches_recommendation(event, target)
        resume_variant = _match_resume_variant(profile, normalized_files[0] if normalized_files else None)

        event_records.append(
            {
                "trace_version": TRACE_SCHEMA_VERSION,
                "record_type": "greenhouse_manual_field_event",
                "manual_session_id": manual_session_id,
                "captured_at": captured_at,
                "application_key": application_key,
                "company_name": analysis.schema.company_name,
                "job_title": analysis.schema.title,
                "board_token": analysis.schema.detection.board_token,
                "job_id": analysis.schema.detection.job_id,
                "public_url": analysis.schema.public_url,
                "page_url": str(event.get("page_url") or final_page_url),
                "sequence": event.get("sequence"),
                "offset_ms": event.get("offset_ms"),
                "event_type": event_type or None,
                "question_label": question_label or None,
                "normalized_label": label_key or None,
                "api_name": api_name or None,
                "element_id": str(event.get("element_id") or "").strip() or None,
                "element_name": str(event.get("element_name") or "").strip() or None,
                "tag_name": str(event.get("tag_name") or "").strip().lower() or None,
                "input_type": str(event.get("input_type") or "").strip().lower() or None,
                "required": question.required if question else bool(event.get("required")),
                "question_group": question.group if question else None,
                "decision_action": question.decision.action if question and question.decision else None,
                "decision_reason": question.decision.reason if question and question.decision else None,
                "recommended_value_source": target.value_source if target else None,
                "recommended_profile_key": target.profile_key if target else None,
                "recommended_value_preview": _preview_target_value(target),
                "draft_source": suggested.get("draft_source") if suggested else None,
                "retrieved_chunk_ids": suggested.get("retrieved_chunk_ids") if suggested else [],
                "style_snippet_ids": suggested.get("style_snippet_ids") if suggested else [],
                "retrieval_summary": suggested.get("retrieval_summary") if suggested else [],
                "matched_recommendation": matched_recommendation,
                "file_names": normalized_files or None,
                "resume_variant_match": resume_variant,
                "checked": event.get("checked"),
                "value_preview": payload["value_preview"],
                "stored_value": payload["stored_value"],
                "value_hash": payload["value_hash"],
            }
        )

    final_answers = _final_manual_answers(observed_events)
    matched_count = 0
    override_count = 0
    manual_only_count = 0
    queue_answer_count = 0
    review_answer_count = 0
    resume_selected_file: str | None = None
    resume_selected_variant: str | None = None

    for question_key, event in final_answers.items():
        question_label = str(event.get("question_label") or "").strip()
        label_key = _normalized_text(question_label)
        question = question_by_key.get(question_key) or question_by_label.get(label_key)
        target = target_by_question.get(question_key) or target_by_label.get(label_key)
        matched = _manual_matches_recommendation(event, target)
        if target is None:
            manual_only_count += 1
        elif matched:
            matched_count += 1
        else:
            override_count += 1
        if question and question.decision:
            if question.decision.action == "QUEUE":
                queue_answer_count += 1
            if question.decision.action == "REVIEW":
                review_answer_count += 1
        file_names = event.get("file_names")
        if isinstance(file_names, list) and file_names and "resume" in label_key and resume_selected_file is None:
            resume_selected_file = str(file_names[0]).strip()
            resume_selected_variant = _match_resume_variant(profile, resume_selected_file)

    assistant_resume_target = next(
        (target for target in fill_targets if target.profile_key == "resume_path"),
        None,
    )
    assistant_resume_file = Path(assistant_resume_target.value).name if assistant_resume_target else None
    assistant_resume_variant = _match_resume_variant(profile, assistant_resume_file)

    session_record = {
        "trace_version": TRACE_SCHEMA_VERSION,
        "record_type": "greenhouse_manual_session",
        "manual_session_id": manual_session_id,
        "captured_at": captured_at,
        "application_key": application_key,
        "requested_url": requested_url,
        "public_url": analysis.schema.public_url,
        "final_page_url": final_page_url,
        "company_name": analysis.schema.company_name,
        "job_title": analysis.schema.title,
        "board_token": analysis.schema.detection.board_token,
        "job_id": analysis.schema.detection.job_id,
        "schema_source": analysis.schema.source,
        "candidate": _profile_snapshot(profile, fill_targets),
        "run_resume_selection": {
            "selected_variant": assistant_resume_variant,
            "selected_file": assistant_resume_file,
            "source": str((profile or {}).get("resume_selection_source") or "").strip() or None,
        },
        "assistant_recommendation": {
            "target_count": len(fill_targets),
            "value_source_counts": _count([target.value_source for target in fill_targets]),
            "resume_file": assistant_resume_file,
            "resume_variant": assistant_resume_variant,
        },
        "manual_resume_decision": {
            "selected_file": resume_selected_file,
            "selected_variant": resume_selected_variant,
            "decision_source": (resume_decision_source or "").strip() or None,
            "external_recommendation": (external_resume_recommendation or "").strip() or None,
            "matches_assistant_recommendation": (
                None
                if assistant_resume_file is None or resume_selected_file is None
                else assistant_resume_file.strip().lower() == resume_selected_file.strip().lower()
            ),
        },
        "observation_summary": {
            "event_count": len(event_records),
            "event_type_counts": _count(event_types),
            "touched_question_count": len(touched_questions),
            "final_answer_count": len(final_answers),
            "matched_recommendation_count": matched_count,
            "override_count": override_count,
            "manual_only_answer_count": manual_only_count,
            "queue_answer_count": queue_answer_count,
            "review_answer_count": review_answer_count,
            "focus_order": focus_order[:50],
        },
        "confirmation_detected": confirmation_detected,
        "submitted": confirmation_detected,
        "ended_reason": (ended_reason or "").strip() or None,
    }
    return session_record, event_records


def append_greenhouse_manual_observation(
    *,
    analysis: GreenhouseApplicationAnalysis,
    observed_events: list[dict[str, Any]],
    profile: dict[str, Any] | None,
    fill_targets: list[GreenhouseFillTarget],
    application_key: str,
    requested_url: str,
    final_page_url: str | None = None,
    confirmation_detected: bool = False,
    ended_reason: str | None = None,
    resume_decision_source: str | None = None,
    external_resume_recommendation: str | None = None,
    session_file: str | Path = DEFAULT_GREENHOUSE_MANUAL_SESSION_FILE,
    field_events_file: str | Path = DEFAULT_GREENHOUSE_MANUAL_FIELD_EVENTS_FILE,
) -> dict[str, Any]:
    session_path = _absolute_path(session_file)
    events_path = _absolute_path(field_events_file)
    session_record, event_records = build_greenhouse_manual_observation_trace(
        analysis=analysis,
        observed_events=observed_events,
        profile=profile,
        fill_targets=fill_targets,
        application_key=application_key,
        requested_url=requested_url,
        final_page_url=final_page_url,
        confirmation_detected=confirmation_detected,
        ended_reason=ended_reason,
        resume_decision_source=resume_decision_source,
        external_resume_recommendation=external_resume_recommendation,
    )
    _append_jsonl(session_path, [session_record])
    _append_jsonl(events_path, event_records)
    return {
        "stored": True,
        "manual_session_id": session_record["manual_session_id"],
        "session_file": str(session_path),
        "field_events_file": str(events_path),
        "session_record_count": 1,
        "field_event_record_count": len(event_records),
    }
