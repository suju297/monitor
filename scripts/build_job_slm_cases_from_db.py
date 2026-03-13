#!/usr/bin/env python3
"""Build a silver-label benchmark set from jobs.db for local SLM evaluation."""

from __future__ import annotations

import argparse
import json
import random
import sqlite3
from pathlib import Path
from typing import Any


def fetch_rows(db_path: str, min_description: int) -> list[dict[str, Any]]:
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    try:
        rows = conn.execute(
            """
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
            WHERE TRIM(COALESCE(title, '')) <> ''
              AND TRIM(COALESCE(location, '')) <> ''
              AND TRIM(COALESCE(description, '')) <> ''
              AND LENGTH(description) >= ?
            """,
            (min_description,),
        ).fetchall()
        return [dict(row) for row in rows]
    finally:
        conn.close()


def looks_like_benchmark_noise(row: dict[str, Any]) -> bool:
    title = str(row.get("title") or "").strip().lower()
    if not title:
        return True
    noise_patterns = (
        "learn more",
        "possible opening",
        "volunteerism",
        "reasonable accommodation",
        "drug free workplace",
        "ai at work",
    )
    return any(pattern in title for pattern in noise_patterns)


def choose(rows: list[dict[str, Any]], count: int, rng: random.Random) -> list[dict[str, Any]]:
    if count <= 0 or not rows:
        return []
    if len(rows) <= count:
        return list(rows)
    return rng.sample(rows, count)


def build_cases(rows: list[dict[str, Any]], positive_limit: int, negative_limit: int, blocked_limit: int, friendly_limit: int, seed: int) -> list[dict[str, Any]]:
    usable = [row for row in rows if not looks_like_benchmark_noise(row)]
    rng = random.Random(seed)

    positives = [row for row in usable if int(row.get("feed_relevance_ok") or 0) == 1]
    negatives = [row for row in usable if int(row.get("feed_relevance_ok") or 0) == 0]
    blocked = [row for row in usable if str(row.get("work_auth_status") or "").strip().lower() == "blocked"]
    friendly = [row for row in usable if str(row.get("work_auth_status") or "").strip().lower() == "friendly"]

    selected: dict[str, dict[str, Any]] = {}
    for bucket, limit in (
        (blocked, blocked_limit),
        (friendly, friendly_limit),
        (positives, positive_limit),
        (negatives, negative_limit),
    ):
        for row in choose(bucket, limit, rng):
            selected[str(row["fingerprint"])] = row

    cases: list[dict[str, Any]] = []
    for idx, row in enumerate(sorted(selected.values(), key=lambda item: str(item["fingerprint"])), start=1):
        work_auth = str(row.get("work_auth_status") or "unknown").strip().lower()
        if work_auth not in {"blocked", "friendly", "unknown"}:
            work_auth = "unknown"
        cases.append(
            {
                "id": f"db_{idx:03d}",
                "fingerprint": row["fingerprint"],
                "company": row.get("company") or "",
                "title": row.get("title") or "",
                "location": row.get("location") or "",
                "posted_at": row.get("posted_at") or "Unknown",
                "description": row.get("description") or "",
                "expected_role_fit": bool(row.get("feed_relevance_ok")),
                "expected_work_auth": work_auth,
                "active": bool(row.get("active")),
                "label_source": "jobs.db_silver",
            }
        )
    return cases


def main() -> int:
    parser = argparse.ArgumentParser(description="Build a benchmark case set from jobs.db")
    parser.add_argument("--db", default=".state/jobs.db", help="Path to jobs.db")
    parser.add_argument("--out", default=".state/job_slm_cases_from_db.json", help="Output JSON path")
    parser.add_argument("--positive-limit", type=int, default=24, help="Max positive role-fit cases")
    parser.add_argument("--negative-limit", type=int, default=24, help="Max negative role-fit cases")
    parser.add_argument("--blocked-limit", type=int, default=10, help="Max blocked work-auth cases")
    parser.add_argument("--friendly-limit", type=int, default=4, help="Max friendly work-auth cases")
    parser.add_argument("--min-description", type=int, default=160, help="Minimum description length")
    parser.add_argument("--seed", type=int, default=20260306, help="Sampling seed")
    args = parser.parse_args()

    rows = fetch_rows(args.db, args.min_description)
    cases = build_cases(
        rows=rows,
        positive_limit=args.positive_limit,
        negative_limit=args.negative_limit,
        blocked_limit=args.blocked_limit,
        friendly_limit=args.friendly_limit,
        seed=args.seed,
    )
    payload = {
        "source": "jobs.db_silver_labels",
        "db_path": args.db,
        "seed": args.seed,
        "min_description": args.min_description,
        "cases": cases,
    }
    Path(args.out).write_text(json.dumps(payload, indent=2), encoding="utf-8")

    positives = sum(1 for case in cases if case["expected_role_fit"])
    negatives = len(cases) - positives
    blocked = sum(1 for case in cases if case["expected_work_auth"] == "blocked")
    friendly = sum(1 for case in cases if case["expected_work_auth"] == "friendly")
    print(
        f"saved {len(cases)} cases to {args.out} "
        f"(positives={positives}, negatives={negatives}, blocked={blocked}, friendly={friendly})"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
