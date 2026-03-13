#!/usr/bin/env python3
"""Benchmark local SLMs for the live ambiguity-only job scorer."""

from __future__ import annotations

import argparse
import json
import statistics
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


DEFAULT_CASES: list[dict[str, Any]] = [
    {
        "id": "c01",
        "title": "Software Engineer II, Full Stack",
        "location": "New York, NY, United States",
        "posted_at": "Posted today",
        "description": "Build React + Go services on AWS.",
        "expected_role_fit": True,
        "expected_internship_status": "not_applicable",
    },
    {
        "id": "c02",
        "title": "Software Engineering Intern",
        "location": "San Francisco, CA",
        "posted_at": "Posted today",
        "description": "Summer internship for current students returning to school after the internship.",
        "expected_role_fit": True,
        "expected_internship_status": "blocked",
    },
    {
        "id": "c03",
        "title": "PhD Machine Learning Engineer Intern",
        "location": "Seattle, WA",
        "posted_at": "Posted today",
        "description": "Research internship on ML systems. Eligibility for a graduated candidate is unclear from the text.",
        "expected_role_fit": True,
        "expected_internship_status": "unknown",
    },
    {
        "id": "c04",
        "title": "Account Executive, Enterprise",
        "location": "Chicago, IL",
        "posted_at": "Posted today",
        "description": "Own sales pipeline and close enterprise deals.",
        "expected_role_fit": False,
        "expected_internship_status": "not_applicable",
    },
]

RESPONSE_SCHEMA: dict[str, Any] = {
    "type": "object",
    "properties": {
        "role_fit": {"type": "boolean"},
        "internship_status": {"type": "string", "enum": ["allowed", "blocked", "unknown", "not_applicable"]},
        "reasons": {"type": "array", "items": {"type": "string"}},
    },
    "required": ["role_fit", "internship_status", "reasons"],
    "additionalProperties": False,
}

DEFAULT_SYSTEM_PROMPT = """
You review borderline software jobs for this candidate profile.

Deterministic filters already handled explicit blockers. Your job is only to resolve ambiguity for:
- borderline role fit within software/backend/full stack/cloud/platform/AI/ML/data targets
- internship eligibility for an already-graduated candidate when student-status wording is unclear

Candidate profile:
- Looking for (ANY ONE is enough): full stack, backend software engineering, cloud/platform/devops, AI/ML, data engineering, data analyst
- Seniority target: early careers through Software Engineer I/II/III
- US-based jobs only
- Recency: prefer jobs posted within the last 7 days when the date is available; unknown dates are allowed but are a weaker signal than explicitly recent jobs

Important:
- `role_fit` is ONLY about role/level/location/recency fit within the target software/data profile.
- `internship_status` is ONLY for internship/co-op roles and decides whether an already-graduated candidate is eligible based on student-status language.
- A job can be `role_fit=true` and `internship_status=blocked` at the same time.
- Do NOT do work-authorization classification. That is already handled deterministically elsewhere.
- Prefer `role_fit=true` when the text clearly supports an in-scope engineering/data role, even if the title is broad or nonstandard.
- Prefer `role_fit=false` when the text indicates consulting, sales engineering, solutions architecture, customer advisory, or a clearly out-of-scope function.
- For internships/co-ops, return `internship_status=blocked` when current enrollment, active degree pursuit, student status, remaining semesters, future graduation window, or return-to-school language is required.
- For internships/co-ops, return `internship_status=allowed` when recent graduates/completed-degree candidates are explicitly allowed or the text makes clear the role is not restricted to current students.
- For internships/co-ops, return `internship_status=unknown` when eligibility for an already-graduated candidate is unclear from the provided text.
- Do not return `internship_status=blocked` only because explicit graduate-friendly wording is missing.
- For non-internships, return `internship_status=not_applicable`.

Return strict JSON only with this schema:
{
  "role_fit": boolean,
  "internship_status": "allowed"|"blocked"|"unknown"|"not_applicable",
  "reasons": ["short reason", "..."]
}
Do not add markdown or extra keys.
""".strip()


def normalize_text_snippet(value: Any, limit: int) -> str:
    text = " ".join(str(value or "").split())
    if limit > 0 and len(text) > limit:
        return text[: limit - 1].rstrip() + "…"
    return text


def post_chat(url: str, payload: dict[str, Any], timeout_s: int) -> dict[str, Any]:
    req = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        return json.loads(resp.read().decode("utf-8"))


def load_cases(path: str | None) -> list[dict[str, Any]]:
    if not path:
        return list(DEFAULT_CASES)
    raw = json.loads(Path(path).read_text(encoding="utf-8"))
    if isinstance(raw, dict):
        raw = raw.get("cases", [])
    if not isinstance(raw, list):
        raise SystemExit(f"Invalid cases file format in {path}")
    cases: list[dict[str, Any]] = []
    for idx, case in enumerate(raw, start=1):
        if not isinstance(case, dict):
            raise SystemExit(f"Case #{idx} in {path} is not an object")
        required = {"title", "location", "posted_at", "description", "expected_role_fit"}
        missing = required.difference(case.keys())
        if missing:
            raise SystemExit(f"Case #{idx} in {path} missing fields: {sorted(missing)}")
        hydrated = dict(case)
        hydrated.setdefault("id", f"case_{idx:03d}")
        hydrated.setdefault("expected_internship_status", "not_applicable")
        cases.append(hydrated)
    return cases


def load_system_prompt(path: str | None) -> str:
    if not path:
        return DEFAULT_SYSTEM_PROMPT
    return Path(path).read_text(encoding="utf-8").strip()


def call_model(url: str, model: str, case: dict[str, Any], timeout_s: int, system_prompt: str) -> tuple[dict[str, Any] | None, float, str | None]:
    user_prompt = (
        f"Title: {normalize_text_snippet(case['title'], 320)}\n"
        f"Location: {normalize_text_snippet(case['location'], 320)}\n"
        f"Posted: {normalize_text_snippet(case['posted_at'], 120)}\n"
        f"Description: {normalize_text_snippet(case['description'], 2200)}\n"
    )
    payload = {
        "model": model,
        "stream": False,
        "format": RESPONSE_SCHEMA,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
        "options": {"temperature": 0},
    }
    if model.lower().startswith("qwen3"):
        # Qwen3 enables thinking by default on Ollama. Disable it for
        # structured classification so latency and JSON adherence are comparable.
        payload["think"] = False
    started = time.perf_counter()
    try:
        response = post_chat(url, payload, timeout_s)
    except urllib.error.URLError as exc:
        return None, time.perf_counter() - started, f"network_error: {exc}"
    except TimeoutError as exc:
        return None, time.perf_counter() - started, f"timeout: {exc}"
    except Exception as exc:  # broad by design for benchmark resilience
        return None, time.perf_counter() - started, f"request_error: {exc}"

    latency = time.perf_counter() - started
    content = ((response.get("message") or {}).get("content") or "").strip()
    if not content:
        return None, latency, "empty_response"
    try:
        parsed = json.loads(content)
    except json.JSONDecodeError as exc:
        return None, latency, f"json_decode_error: {exc}"
    return parsed, latency, None


def median(values: list[float]) -> float:
    return statistics.median(values) if values else 0.0


def run_benchmark(models: list[str], url: str, timeout_s: int, cases: list[dict[str, Any]], system_prompt: str) -> dict[str, Any]:
    out: dict[str, Any] = {"cases": cases, "results": {}, "system_prompt": system_prompt}
    for model in models:
        rows: list[dict[str, Any]] = []
        latencies: list[float] = []
        role_correct = 0
        internship_correct = 0
        both_correct = 0
        parse_ok = 0
        internship_totals = {"allowed": 0, "blocked": 0, "unknown": 0, "not_applicable": 0}
        internship_hits = {"allowed": 0, "blocked": 0, "unknown": 0, "not_applicable": 0}

        for case in cases:
            parsed, latency_s, err = call_model(url, model, case, timeout_s, system_prompt)
            row: dict[str, Any] = {
                "id": case["id"],
                "latency_s": round(latency_s, 3),
                "error": err,
                "parsed": parsed,
            }
            latencies.append(latency_s)

            if parsed is None:
                rows.append(row)
                continue

            parse_ok += 1
            predicted_role_fit = bool(parsed.get("role_fit"))
            predicted_internship = str(parsed.get("internship_status", "")).strip().lower()
            expected_role_fit = bool(case["expected_role_fit"])
            expected_internship = str(case.get("expected_internship_status", "not_applicable")).strip().lower()

            row["role_fit_correct"] = predicted_role_fit == expected_role_fit
            row["internship_correct"] = predicted_internship == expected_internship

            if row["role_fit_correct"]:
                role_correct += 1
            if row["internship_correct"]:
                internship_correct += 1
            if row["role_fit_correct"] and row["internship_correct"]:
                both_correct += 1

            if expected_internship in internship_totals:
                internship_totals[expected_internship] += 1
                if predicted_internship == expected_internship:
                    internship_hits[expected_internship] += 1

            rows.append(row)

        total = len(cases)
        parse_rate = parse_ok / total if total else 0.0
        role_acc = role_correct / total if total else 0.0
        internship_acc = internship_correct / total if total else 0.0
        both_acc = both_correct / total if total else 0.0
        internship_recalls = {
            key: (internship_hits[key] / internship_totals[key] if internship_totals[key] else 0.0)
            for key in internship_totals
        }

        out["results"][model] = {
            "summary": {
                "cases": total,
                "parse_ok": parse_ok,
                "parse_rate": round(parse_rate, 4),
                "role_fit_accuracy": round(role_acc, 4),
                "internship_accuracy": round(internship_acc, 4),
                "joint_accuracy": round(both_acc, 4),
                "internship_blocked_recall": round(internship_recalls["blocked"], 4),
                "internship_unknown_recall": round(internship_recalls["unknown"], 4),
                "internship_not_applicable_recall": round(internship_recalls["not_applicable"], 4),
                "internship_allowed_recall": round(internship_recalls["allowed"], 4),
                "latency_avg_s": round(sum(latencies) / len(latencies), 3) if latencies else 0.0,
                "latency_median_s": round(median(latencies), 3),
                "latency_p95_s": round(sorted(latencies)[max(0, int(len(latencies) * 0.95) - 1)], 3) if latencies else 0.0,
            },
            "rows": rows,
        }
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description="Benchmark local SLMs for the live ambiguity-only job scorer")
    parser.add_argument(
        "--models",
        default="qwen2.5:3b,ministral-3:3b",
        help="Comma-separated model list (default: qwen2.5:3b,ministral-3:3b)",
    )
    parser.add_argument(
        "--url",
        default="http://127.0.0.1:11434/api/chat",
        help="Ollama chat endpoint",
    )
    parser.add_argument("--timeout", type=int, default=45, help="Per-case timeout seconds")
    parser.add_argument("--out", default=".state/slm_benchmark_results.json", help="Output JSON path")
    parser.add_argument("--cases-file", default="", help="Optional JSON file containing benchmark cases")
    parser.add_argument("--prompt-file", default="", help="Optional text file overriding the default system prompt")
    args = parser.parse_args()

    models = [m.strip() for m in args.models.split(",") if m.strip()]
    if not models:
        raise SystemExit("No models provided.")
    cases = load_cases(args.cases_file or None)
    system_prompt = load_system_prompt(args.prompt_file or None)

    results = run_benchmark(models=models, url=args.url, timeout_s=args.timeout, cases=cases, system_prompt=system_prompt)
    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(results, f, indent=2)

    for model in models:
        summary = results["results"][model]["summary"]
        print(
            f"{model}: parse={summary['parse_ok']}/{summary['cases']} "
            f"role_acc={summary['role_fit_accuracy']:.2f} "
            f"internship_acc={summary['internship_accuracy']:.2f} "
            f"joint={summary['joint_accuracy']:.2f} "
            f"internship_blocked_recall={summary['internship_blocked_recall']:.2f} "
            f"internship_unknown_recall={summary['internship_unknown_recall']:.2f} "
            f"avg_s={summary['latency_avg_s']:.2f} median_s={summary['latency_median_s']:.2f}"
        )
    print(f"saved: {args.out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
