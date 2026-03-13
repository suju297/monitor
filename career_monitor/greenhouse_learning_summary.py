from __future__ import annotations

import json
import re
from collections import Counter
from pathlib import Path
from typing import Any

from .greenhouse_assistant import detect_greenhouse_application
from .greenhouse_submission_state import build_greenhouse_application_key

DEFAULT_LEARNING_LIMIT = 8
MAX_LEARNING_LIMIT = 20
_EXCLUDED_QUESTION_GROUPS = {"compliance", "demographic_questions", "data_compliance"}
_EXCLUDED_UPLOAD_LABELS = {"attach", "resume", "resume cv", "resume cv upload", "cover letter"}


def _normalized_text(value: Any) -> str:
    return re.sub(r"\s+", " ", re.sub(r"[^a-z0-9]+", " ", str(value or "").strip().lower())).strip()


def _absolute_path(path: str | Path) -> Path:
    resolved = Path(path).expanduser()
    if not resolved.is_absolute():
        resolved = Path.cwd() / resolved
    return resolved


def _read_jsonl_records(path: str | Path) -> list[dict[str, Any]]:
    record_path = _absolute_path(path)
    if not record_path.exists():
        return []
    records: list[dict[str, Any]] = []
    for line in record_path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        try:
            payload = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(payload, dict):
            records.append(payload)
    return records


def _application_key_for_url(url: str | None) -> str | None:
    normalized_url = str(url or "").strip()
    if not normalized_url:
        return None
    detection = detect_greenhouse_application(normalized_url)
    if not detection.is_greenhouse or not detection.is_application:
        return None
    return build_greenhouse_application_key(
        board_token=detection.board_token,
        job_id=detection.job_id,
        public_url=normalized_url,
    )


def _top_counts(counter: Counter[str], *, limit: int = 3) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for value, count in counter.most_common(limit):
        rows.append({"value": value, "count": count})
    return rows


def _family_key(event: dict[str, Any]) -> str:
    normalized = _normalized_text(event.get("normalized_label"))
    if normalized:
        return normalized
    return _normalized_text(event.get("question_label"))


def _is_learnable_final_event(event: dict[str, Any]) -> bool:
    if str(event.get("question_group") or "").strip() in _EXCLUDED_QUESTION_GROUPS:
        return False
    if str(event.get("input_type") or "").strip().lower() == "file":
        return False
    file_names = event.get("file_names")
    if isinstance(file_names, list) and file_names:
        return False
    label_key = _family_key(event)
    if label_key in _EXCLUDED_UPLOAD_LABELS:
        return False
    stored_value = str(event.get("stored_value") or "").strip()
    return bool(stored_value)


def _final_manual_events(event_records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    latest_by_question: dict[tuple[str, str], dict[str, Any]] = {}
    for record in event_records:
        if record.get("record_type") != "greenhouse_manual_field_event":
            continue
        event_type = str(record.get("event_type") or "").strip().lower()
        if event_type not in {"change", "blur"}:
            continue
        session_id = str(record.get("manual_session_id") or "").strip()
        family_key = _family_key(record)
        if not session_id or not family_key:
            continue
        record_key = (session_id, family_key)
        sequence = int(record.get("sequence") or 0)
        previous = latest_by_question.get(record_key)
        previous_sequence = int(previous.get("sequence") or 0) if previous else -1
        if sequence >= previous_sequence:
            latest_by_question[record_key] = record
    return list(latest_by_question.values())


def _family_sort_key(item: dict[str, Any], primary_key: str) -> tuple[Any, ...]:
    return (
        -int(item.get(primary_key) or 0),
        -int(item.get("total_answers") or 0),
        str(item.get("question_label") or ""),
    )


def _family_payload(record: dict[str, Any], *, include_candidate: bool = False) -> dict[str, Any]:
    payload = {
        "normalized_label": record["normalized_label"],
        "question_label": record["question_label"],
        "question_group": record.get("question_group"),
        "total_answers": record["total_answers"],
        "recommended_count": record["recommended_count"],
        "matched_count": record["matched_count"],
        "override_count": record["override_count"],
        "manual_only_count": record["manual_only_count"],
        "acceptance_rate": record["acceptance_rate"],
        "top_final_answers": record["top_final_answers"],
        "top_recommended_answers": record["top_recommended_answers"],
        "company_samples": record["company_samples"],
        "last_seen_at": record["last_seen_at"],
    }
    if include_candidate:
        payload["candidate"] = record.get("candidate")
    return payload


def greenhouse_learning_summary_payload(
    *,
    session_file: str | Path,
    field_events_file: str | Path,
    requested_url: str | None = None,
    limit: int = DEFAULT_LEARNING_LIMIT,
) -> dict[str, Any]:
    normalized_limit = max(1, min(MAX_LEARNING_LIMIT, int(limit or DEFAULT_LEARNING_LIMIT)))
    application_key = _application_key_for_url(requested_url)
    session_records = [
        record
        for record in _read_jsonl_records(session_file)
        if record.get("record_type") == "greenhouse_manual_session"
    ]
    if application_key:
        session_records = [record for record in session_records if record.get("application_key") == application_key]
    elif requested_url:
        normalized_url = str(requested_url).strip()
        session_records = [
            record
            for record in session_records
            if normalized_url
            in {
                str(record.get("requested_url") or "").strip(),
                str(record.get("public_url") or "").strip(),
                str(record.get("final_page_url") or "").strip(),
            }
        ]
    session_records.sort(key=lambda item: str(item.get("captured_at") or ""), reverse=True)
    session_ids = {
        str(record.get("manual_session_id") or "").strip()
        for record in session_records
        if str(record.get("manual_session_id") or "").strip()
    }
    event_records = [
        record
        for record in _read_jsonl_records(field_events_file)
        if record.get("record_type") == "greenhouse_manual_field_event"
        and str(record.get("manual_session_id") or "").strip() in session_ids
    ]
    final_events = _final_manual_events(event_records)

    summary = {
        "session_count": len(session_records),
        "submitted_count": sum(1 for record in session_records if record.get("submitted")),
        "assistant_review_session_count": sum(
            1
            for record in session_records
            if str((record.get("manual_resume_decision") or {}).get("decision_source") or "").strip() == "assistant_review"
        ),
        "field_event_count": len(event_records),
        "final_answer_count": len(final_events),
        "matched_recommendation_count": 0,
        "override_count": 0,
        "manual_only_answer_count": 0,
    }

    family_stats: dict[str, dict[str, Any]] = {}
    for event in final_events:
        if not _is_learnable_final_event(event):
            continue
        question_group = str(event.get("question_group") or "").strip()
        family_key = _family_key(event)
        if not family_key:
            continue
        family = family_stats.setdefault(
            family_key,
            {
                "normalized_label": family_key,
                "question_label_counts": Counter(),
                "question_group": question_group or None,
                "company_samples": set(),
                "total_answers": 0,
                "recommended_count": 0,
                "matched_count": 0,
                "override_count": 0,
                "manual_only_count": 0,
                "final_answer_counts": Counter(),
                "recommended_answer_counts": Counter(),
                "last_seen_at": "",
            },
        )
        family["total_answers"] += 1
        question_label = str(event.get("question_label") or "").strip()
        if question_label:
            family["question_label_counts"][question_label] += 1
        company_name = str(event.get("company_name") or "").strip()
        if company_name:
            family["company_samples"].add(company_name)
        captured_at = str(event.get("captured_at") or "").strip()
        if captured_at > family["last_seen_at"]:
            family["last_seen_at"] = captured_at

        recommended_source = str(event.get("recommended_value_source") or "").strip()
        if recommended_source:
            family["recommended_count"] += 1
        matched = event.get("matched_recommendation")
        if matched is True:
            family["matched_count"] += 1
            summary["matched_recommendation_count"] += 1
        elif matched is False:
            family["override_count"] += 1
            summary["override_count"] += 1
        else:
            family["manual_only_count"] += 1
            summary["manual_only_answer_count"] += 1

        stored_value = str(event.get("stored_value") or "").strip()
        if stored_value:
            family["final_answer_counts"][stored_value] += 1
        recommended_value = str(event.get("recommended_value_preview") or "").strip()
        if recommended_value:
            family["recommended_answer_counts"][recommended_value] += 1

    family_rows: list[dict[str, Any]] = []
    for family in family_stats.values():
        question_label = family["question_label_counts"].most_common(1)[0][0] if family["question_label_counts"] else family["normalized_label"]
        top_answer = family["final_answer_counts"].most_common(1)
        top_answer_value = top_answer[0][0] if top_answer else None
        top_answer_count = top_answer[0][1] if top_answer else 0
        acceptance_rate = (
            family["matched_count"] / family["recommended_count"] if family["recommended_count"] else 0.0
        )
        candidate: dict[str, Any] | None = None
        if top_answer_value and top_answer_count >= 2:
            dominance = top_answer_count / family["total_answers"]
            if family["override_count"] >= 2 and dominance >= 0.75:
                candidate = {
                    "type": "promote_default",
                    "answer": top_answer_value,
                    "support": top_answer_count,
                    "confidence": round(dominance, 3),
                    "reason": "Repeated overrides converged on the same final answer.",
                }
            elif family["manual_only_count"] >= 2 and dominance >= 0.8:
                candidate = {
                    "type": "candidate_profile_default",
                    "answer": top_answer_value,
                    "support": top_answer_count,
                    "confidence": round(dominance, 3),
                    "reason": "Repeated manual answers converged without an assistant recommendation.",
                }
        if candidate is None and family["matched_count"] >= 3 and acceptance_rate >= 0.9:
            top_recommended = family["recommended_answer_counts"].most_common(1)
            candidate = {
                "type": "promote_rule",
                "answer": top_recommended[0][0] if top_recommended else None,
                "support": family["matched_count"],
                "confidence": round(acceptance_rate, 3),
                "reason": "Assistant recommendation was consistently accepted.",
            }

        family_rows.append(
            {
                "normalized_label": family["normalized_label"],
                "question_label": question_label,
                "question_group": family["question_group"],
                "total_answers": family["total_answers"],
                "recommended_count": family["recommended_count"],
                "matched_count": family["matched_count"],
                "override_count": family["override_count"],
                "manual_only_count": family["manual_only_count"],
                "acceptance_rate": round(acceptance_rate, 3),
                "top_final_answers": _top_counts(family["final_answer_counts"]),
                "top_recommended_answers": _top_counts(family["recommended_answer_counts"]),
                "company_samples": sorted(family["company_samples"])[:3],
                "last_seen_at": family["last_seen_at"] or None,
                "candidate": candidate,
            }
        )

    top_overrides = sorted(
        [row for row in family_rows if row["override_count"] > 0],
        key=lambda item: _family_sort_key(item, "override_count"),
    )[:normalized_limit]
    top_acceptances = sorted(
        [row for row in family_rows if row["matched_count"] > 0],
        key=lambda item: (-int(item["matched_count"]), -float(item["acceptance_rate"]), str(item["question_label"])),
    )[:normalized_limit]
    stable_defaults = sorted(
        [
            row
            for row in family_rows
            if row["top_final_answers"]
            and row["top_final_answers"][0]["count"] >= 2
            and row["top_final_answers"][0]["count"] / max(1, row["total_answers"]) >= 0.8
        ],
        key=lambda item: (
            -(item["top_final_answers"][0]["count"] if item["top_final_answers"] else 0),
            -int(item["total_answers"]),
            str(item["question_label"]),
        ),
    )[:normalized_limit]
    learning_candidates = sorted(
        [row for row in family_rows if row.get("candidate")],
        key=lambda item: (
            -int((item.get("candidate") or {}).get("support") or 0),
            -float((item.get("candidate") or {}).get("confidence") or 0.0),
            str(item.get("question_label") or ""),
        ),
    )[:normalized_limit]

    return {
        "ok": True,
        "requested_url": str(requested_url or "").strip() or None,
        "application_key": application_key,
        "summary": summary,
        "top_overrides": [_family_payload(item) for item in top_overrides],
        "top_acceptances": [_family_payload(item) for item in top_acceptances],
        "stable_defaults": [_family_payload(item) for item in stable_defaults],
        "learning_candidates": [_family_payload(item, include_candidate=True) for item in learning_candidates],
    }
