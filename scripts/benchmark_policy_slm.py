#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import time
import urllib.error
import urllib.request
from pathlib import Path


SYSTEM_PROMPT = (
    'You are a source-plan router for job crawling. Return strict JSON only with shape '
    '{"plan":["source1","source2"]}. Use only sources from allowed_plan. No commentary.'
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Benchmark policy routing models against a gold dataset.")
    parser.add_argument("--cases", required=True, help="Gold dataset JSON file.")
    parser.add_argument("--models", required=True, help="Comma-separated Ollama models.")
    parser.add_argument(
        "--endpoint",
        default="http://127.0.0.1:11434/api/chat",
        help="Ollama chat endpoint.",
    )
    parser.add_argument("--timeout-seconds", type=int, default=20, help="Per-request timeout.")
    parser.add_argument("--out", required=True, help="Output JSON report.")
    return parser.parse_args()


def extract_json_object(text: str) -> str:
    text = text.strip()
    if text.startswith("{") and text.endswith("}"):
        return text
    start = text.find("{")
    end = text.rfind("}")
    if start >= 0 and end > start:
        return text[start : end + 1].strip()
    return ""


def unique_plan(values: list[str], allowed_plan: list[str]) -> list[str]:
    allowed = {value.strip().lower() for value in allowed_plan if value.strip()}
    out: list[str] = []
    for value in values:
        normalized = value.strip().lower()
        if not normalized or normalized not in allowed or normalized in out:
            continue
        out.append(normalized)
    return out


def ollama_payload(model: str, payload: dict) -> dict:
    body = {
        "model": model,
        "stream": False,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": f"Choose source plan for this payload: {json.dumps(payload, separators=(',', ':'))}"},
        ],
    }
    if model.strip().lower().startswith("qwen3"):
        body["think"] = False
    return body


def call_model(model: str, payload: dict, endpoint: str, timeout_seconds: int) -> tuple[list[str], float, str | None]:
    started = time.perf_counter()
    req = urllib.request.Request(
        endpoint,
        data=json.dumps(ollama_payload(model, payload)).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout_seconds) as resp:
            raw = resp.read()
    except urllib.error.URLError as exc:
        return [], time.perf_counter() - started, f"url_error: {exc}"
    except Exception as exc:  # noqa: BLE001
        return [], time.perf_counter() - started, f"request_error: {exc}"

    latency = time.perf_counter() - started
    try:
        parsed = json.loads(raw.decode("utf-8"))
    except json.JSONDecodeError as exc:
        return [], latency, f"response_json_error: {exc}"

    content = str(((parsed.get("message") or {}).get("content") or "")).strip()
    if not content:
        return [], latency, "empty_content"
    candidate = extract_json_object(content)
    if not candidate:
        return [], latency, "no_json_object"
    try:
        out = json.loads(candidate)
    except json.JSONDecodeError as exc:
        return [], latency, f"plan_json_error: {exc}"
    plan = out.get("plan")
    if not isinstance(plan, list):
        return [], latency, "missing_plan"
    return unique_plan([str(v) for v in plan], payload.get("allowed_plan", [])), latency, None


def configured_baseline(payload: dict) -> list[str]:
    company = payload.get("company") or {}
    source = str(company.get("Source") or "").strip().lower()
    fallback = company.get("FallbackSources") or []
    out: list[str] = []
    if source:
        out.append(source)
    out.extend(str(v).strip().lower() for v in fallback if str(v).strip())
    return unique_plan(out, payload.get("allowed_plan", []))


def adaptive_baseline(payload: dict) -> list[str]:
    plan = configured_baseline(payload)
    prior = payload.get("prior_status") or {}
    if str(prior.get("status") or "").strip().lower() != "blocked":
        return plan
    attempted = prior.get("attempted_sources") or []
    if not attempted:
        return plan
    blocked_primary = str(attempted[0]).strip().lower()
    if blocked_primary not in plan:
        return plan
    return [source for source in plan if source != blocked_primary] + [blocked_primary]


def plan_metrics(predicted: list[str], gold: list[str]) -> dict:
    top1 = int(bool(predicted) and bool(gold) and predicted[0] == gold[0])
    exact = int(predicted == gold)
    rr = 0.0
    if gold:
        target = gold[0]
        if target in predicted:
            rr = 1.0 / (predicted.index(target) + 1)
    return {"top1": top1, "exact": exact, "reciprocal_rank": rr}


def summarize(name: str, rows: list[dict]) -> dict:
    total = len(rows)
    if total == 0:
        return {"name": name, "cases": 0}
    return {
        "name": name,
        "cases": total,
        "top1_accuracy": sum(row["metrics"]["top1"] for row in rows) / total,
        "exact_plan_accuracy": sum(row["metrics"]["exact"] for row in rows) / total,
        "mrr_first_source": sum(row["metrics"]["reciprocal_rank"] for row in rows) / total,
        "avg_latency_seconds": sum(row.get("latency_seconds", 0.0) for row in rows) / total,
        "invalid_rate": sum(1 for row in rows if row.get("error")) / total,
    }


def main() -> int:
    args = parse_args()
    dataset = json.loads(Path(args.cases).read_text())
    cases = dataset.get("cases", [])
    model_names = [value.strip() for value in args.models.split(",") if value.strip()]

    report: dict = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "cases_file": args.cases,
        "endpoint": args.endpoint,
        "timeout_seconds": args.timeout_seconds,
        "models": {},
        "baselines": {},
    }

    baseline_rows = {"configured_primary": [], "adaptive_blocked": []}
    for case in cases:
        payload = case["payload"]
        gold = case["gold_plan"]
        for baseline_name, baseline_fn in (
            ("configured_primary", configured_baseline),
            ("adaptive_blocked", adaptive_baseline),
        ):
            predicted = baseline_fn(payload)
            baseline_rows[baseline_name].append(
                {
                    "id": case["id"],
                    "predicted_plan": predicted,
                    "gold_plan": gold,
                    "metrics": plan_metrics(predicted, gold),
                    "latency_seconds": 0.0,
                    "error": None,
                }
            )

    for baseline_name, rows in baseline_rows.items():
        report["baselines"][baseline_name] = {
            "summary": summarize(baseline_name, rows),
            "rows": rows,
        }

    for model in model_names:
        rows = []
        for case in cases:
            predicted, latency, error = call_model(model, case["payload"], args.endpoint, args.timeout_seconds)
            rows.append(
                {
                    "id": case["id"],
                    "predicted_plan": predicted,
                    "gold_plan": case["gold_plan"],
                    "metrics": plan_metrics(predicted, case["gold_plan"]),
                    "latency_seconds": latency,
                    "error": error,
                }
            )
        report["models"][model] = {"summary": summarize(model, rows), "rows": rows}

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(report, indent=2) + "\n")
    print(out_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
