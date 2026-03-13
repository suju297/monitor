#!/usr/bin/env python3
"""Export the active ambiguity slice from jobs.db for hand review."""

from __future__ import annotations

import argparse
import json
import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def normalize_text(value: Any, limit: int) -> str:
    text = " ".join(str(value or "").split())
    if limit > 0 and len(text) > limit:
        return text[: limit - 1].rstrip() + "..."
    return text


def load_rows(db_path: str, description_limit: int) -> list[dict[str, Any]]:
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
                work_auth_status,
                deterministic_role_decision,
                deterministic_internship_status,
                needs_role_slm,
                needs_internship_slm,
                description
            FROM jobs
            WHERE active = 1
              AND (needs_role_slm = 1 OR needs_internship_slm = 1)
            ORDER BY needs_internship_slm DESC, company, title
            """
        ).fetchall()
        payload: list[dict[str, Any]] = []
        for row in rows:
            payload.append(
                {
                    "fingerprint": row["fingerprint"],
                    "company": row["company"] or "",
                    "title": row["title"] or "",
                    "location": row["location"] or "",
                    "posted_at": row["posted_at"] or "",
                    "work_auth_status": (row["work_auth_status"] or "").strip().lower() or "unknown",
                    "deterministic_role_decision": (row["deterministic_role_decision"] or "").strip().lower() or "ambiguous",
                    "deterministic_internship_status": (row["deterministic_internship_status"] or "").strip().lower() or "not_applicable",
                    "needs_role_slm": bool(row["needs_role_slm"]),
                    "needs_internship_slm": bool(row["needs_internship_slm"]),
                    "description": normalize_text(row["description"], description_limit),
                }
            )
        return payload
    finally:
        conn.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Export the active ambiguity slice from jobs.db")
    parser.add_argument("--db", default=".state/jobs.db", help="Path to jobs.db")
    parser.add_argument("--out", required=True, help="Output JSON path")
    parser.add_argument("--description-limit", type=int, default=1800, help="Truncate descriptions to this many characters")
    args = parser.parse_args()

    cases = load_rows(args.db, args.description_limit)
    payload = {
        "metadata": {
            "source": "jobs.db_active_ambiguity_slice",
            "db_path": args.db,
            "generated_at": datetime.now(timezone.utc).isoformat(),
            "description_limit": args.description_limit,
            "case_count": len(cases),
        },
        "cases": cases,
    }
    Path(args.out).write_text(json.dumps(payload, indent=2), encoding="utf-8")
    print(f"saved {len(cases)} ambiguity candidates to {args.out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
