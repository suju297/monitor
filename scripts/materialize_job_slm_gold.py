#!/usr/bin/env python3
"""Materialize a hand-labeled SLM gold dataset from jobs.db."""

from __future__ import annotations

import argparse
import json
import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def normalize_text_snippet(value: Any, limit: int) -> str:
    text = " ".join(str(value or "").split())
    if limit > 0 and len(text) > limit:
        return text[: limit - 1].rstrip() + "..."
    return text


def load_label_spec(path: str) -> dict[str, Any]:
    payload = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise SystemExit(f"Invalid label spec in {path}")
    cases = payload.get("cases")
    if not isinstance(cases, list) or not cases:
        raise SystemExit(f"No cases found in {path}")
    return payload


def load_jobs(db_path: str, fingerprints: list[str]) -> dict[str, dict[str, Any]]:
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    try:
        rows = conn.execute(
            f"""
            SELECT
                fingerprint,
                company,
                title,
                location,
                posted_at,
                description,
                active,
                feed_relevance_ok,
                work_auth_status
            FROM jobs
            WHERE fingerprint IN ({",".join("?" for _ in fingerprints)})
            """,
            fingerprints,
        ).fetchall()
        return {str(row["fingerprint"]): dict(row) for row in rows}
    finally:
        conn.close()


def materialize_case(row: dict[str, Any], label: dict[str, Any], idx: int, description_limit: int) -> dict[str, Any]:
    expected_work_auth = str(label["expected_work_auth"]).strip().lower()
    expected_internship = str(label.get("expected_internship_status", "not_applicable")).strip().lower()
    return {
        "id": f"gold_{idx:03d}",
        "fingerprint": row["fingerprint"],
        "company": row.get("company") or "",
        "title": normalize_text_snippet(row.get("title") or "", 320),
        "location": normalize_text_snippet(row.get("location") or "", 320),
        "posted_at": normalize_text_snippet(row.get("posted_at") or "Unknown", 120),
        "description": normalize_text_snippet(row.get("description") or "", description_limit),
        "expected_role_fit": bool(label["expected_role_fit"]),
        "expected_work_auth": expected_work_auth,
        "expected_internship_status": expected_internship,
        "review_notes": normalize_text_snippet(label.get("review_notes") or "", 240),
        "category": normalize_text_snippet(label.get("category") or "", 80),
        "active_at_review": bool(row.get("active")),
        "silver_feed_relevance_ok": bool(row.get("feed_relevance_ok")),
        "silver_work_auth_status": str(row.get("work_auth_status") or "unknown").strip().lower() or "unknown",
    }


def summarize(cases: list[dict[str, Any]]) -> dict[str, Any]:
    summary = {
        "case_count": len(cases),
        "role_fit_true": sum(1 for case in cases if case["expected_role_fit"]),
        "role_fit_false": sum(1 for case in cases if not case["expected_role_fit"]),
        "work_auth": {},
        "internship_status": {},
    }
    for key in ("blocked", "friendly", "unknown"):
        summary["work_auth"][key] = sum(1 for case in cases if case["expected_work_auth"] == key)
    for key in ("allowed", "blocked", "unknown", "not_applicable"):
        summary["internship_status"][key] = sum(1 for case in cases if case["expected_internship_status"] == key)
    return summary


def main() -> int:
    parser = argparse.ArgumentParser(description="Materialize a hand-labeled SLM gold dataset from jobs.db")
    parser.add_argument("--db", default=".state/jobs.db", help="Path to jobs.db")
    parser.add_argument("--labels", required=True, help="Path to the hand-labeled gold spec JSON")
    parser.add_argument("--out", required=True, help="Output path for the expanded gold dataset JSON")
    parser.add_argument("--description-limit", type=int, default=2200, help="Truncate descriptions to this many characters")
    args = parser.parse_args()

    label_spec = load_label_spec(args.labels)
    labels = label_spec["cases"]
    fingerprints = [str(case["fingerprint"]) for case in labels]
    rows = load_jobs(args.db, fingerprints)

    materialized: list[dict[str, Any]] = []
    missing = [fingerprint for fingerprint in fingerprints if fingerprint not in rows]
    if missing:
        raise SystemExit(f"Missing fingerprints in {args.db}: {missing}")

    for idx, label in enumerate(labels, start=1):
        row = rows[str(label["fingerprint"])]
        materialized.append(materialize_case(row, label, idx, args.description_limit))

    payload = {
        "metadata": {
            "source": "jobs.db_hand_reviewed",
            "db_path": args.db,
            "labels_path": args.labels,
            "generated_at": datetime.now(timezone.utc).isoformat(),
            "description_limit": args.description_limit,
            "notes": [
                "Hand-reviewed against the repo's role-fit and work-authorization policy.",
                "Descriptions are normalized and truncated to stay close to the live scorer input envelope.",
                "Friendly work-authorization examples were not found in the live DB sample on 2026-03-06.",
            ],
            "summary": summarize(materialized),
        },
        "cases": materialized,
    }
    Path(args.out).write_text(json.dumps(payload, indent=2), encoding="utf-8")
    print(f"saved {len(materialized)} gold cases to {args.out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
