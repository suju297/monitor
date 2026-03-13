from __future__ import annotations

import argparse
import json
import re
import statistics
import sys
from collections import Counter
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Any

from .exceptions import CrawlFetchError
from .greenhouse_assistant import GreenhouseApplicationSchema, load_greenhouse_application_schema
from .http_client import perform_request, requests_session
from .constants import DEFAULT_TIMEOUT_SECONDS
from .utils import utc_now
import yaml


def _normalized_text(value: str | None) -> str:
    return re.sub(r"\s+", " ", re.sub(r"[^a-z0-9]+", " ", (value or "").strip().lower())).strip()


def _question_signature(question: dict[str, Any]) -> str:
    parts = [f"{item['ui_type']}:{item['api_type']}" for item in question["inputs"]]
    return " + ".join(parts) if parts else "unknown"


def _load_greenhouse_targets(
    config_path: Path,
    company_filters: list[str] | None = None,
) -> list[dict[str, Any]]:
    if not config_path.exists():
        raise FileNotFoundError(f"Config file not found: {config_path}")
    payload = yaml.safe_load(config_path.read_text(encoding="utf-8")) or {}
    companies = payload.get("companies")
    if not isinstance(companies, list):
        raise CrawlFetchError("Config must contain a 'companies' list.")
    requested = {_normalized_text(item) for item in (company_filters or []) if item.strip()}
    targets: list[dict[str, Any]] = []
    for entry in companies:
        if not isinstance(entry, dict) or bool(entry.get("disabled", False)):
            continue
        if str(entry.get("source", "")).strip().lower() != "greenhouse":
            continue
        name = str(entry.get("name", "")).strip()
        if not name:
            continue
        normalized_name = _normalized_text(name)
        if requested and normalized_name not in requested:
            continue
        board = str(entry.get("greenhouse_board") or "").strip()
        if not board:
            continue
        targets.append(
            {
                "company": name,
                "board": board,
                "timeout_seconds": max(5, int(entry.get("timeout_seconds", DEFAULT_TIMEOUT_SECONDS))),
                "careers_url": str(entry.get("careers_url") or "").strip(),
            }
        )
    if requested and not targets:
        raise CrawlFetchError("No matching Greenhouse companies were found for the requested filters.")
    if not targets:
        raise CrawlFetchError("No Greenhouse companies were found in the configured companies file.")
    return targets


def _fetch_board_jobs(*, board: str, timeout_seconds: int) -> list[dict[str, Any]]:
    session = requests_session()
    endpoint = f"https://boards-api.greenhouse.io/v1/boards/{board}/jobs?content=true"
    response = perform_request(
        session=session,
        method="GET",
        url=endpoint,
        timeout_seconds=timeout_seconds,
    )
    payload = response.json()
    jobs: list[dict[str, Any]] = []
    for item in payload.get("jobs", []):
        job_id = str(item.get("id", "")).strip()
        url = str(item.get("absolute_url") or item.get("url") or "").strip()
        title = str(item.get("title") or "").strip()
        if not job_id or not url or not title:
            continue
        jobs.append(
            {
                "job_id": job_id,
                "url": url,
                "title": title,
                "location": str((item.get("location") or {}).get("name") or "").strip() or None,
                "updated_at": str(item.get("updated_at") or "").strip() or None,
            }
        )
    return jobs


def _job_record_from_schema(
    *,
    company: str,
    board: str,
    job_id: str,
    job_url: str,
    schema: GreenhouseApplicationSchema,
) -> dict[str, Any]:
    questions: list[dict[str, Any]] = []
    required_count = 0
    group_counts: Counter[str] = Counter()
    ui_type_counts: Counter[str] = Counter()
    api_type_counts: Counter[str] = Counter()
    for question in schema.questions:
        if question.required:
            required_count += 1
        group_counts[question.group] += 1
        inputs: list[dict[str, Any]] = []
        for input_field in question.inputs:
            ui_type_counts[input_field.ui_type] += 1
            api_type_counts[input_field.api_type] += 1
            inputs.append(
                {
                    "api_name": input_field.api_name,
                    "api_type": input_field.api_type,
                    "ui_type": input_field.ui_type,
                    "selector": input_field.selector,
                    "options_count": len(input_field.options),
                }
            )
        questions.append(
            {
                "label": question.label,
                "normalized_label": _normalized_text(question.label),
                "group": question.group,
                "required": question.required,
                "description": question.description,
                "input_count": len(inputs),
                "inputs": inputs,
                "signature": " + ".join(
                    f"{item['ui_type']}:{item['api_type']}" for item in inputs
                )
                or "unknown",
            }
        )
    return {
        "company": company,
        "board": board,
        "job_id": job_id,
        "job_url": job_url,
        "title": schema.title,
        "job_location": schema.job_location,
        "question_count": len(questions),
        "required_question_count": required_count,
        "group_counts": dict(group_counts),
        "ui_type_counts": dict(ui_type_counts),
        "api_type_counts": dict(api_type_counts),
        "questions": questions,
    }


def _survey_job(
    *,
    company: str,
    board: str,
    job_id: str,
    url: str,
    timeout_seconds: int,
) -> dict[str, Any]:
    session = requests_session()
    schema_url = f"https://job-boards.greenhouse.io/{board}/jobs/{job_id}"
    schema = load_greenhouse_application_schema(
        schema_url,
        session=session,
        timeout_seconds=timeout_seconds,
    )
    return _job_record_from_schema(
        company=company,
        board=board,
        job_id=job_id,
        job_url=url,
        schema=schema,
    )


def summarize_survey(
    job_records: list[dict[str, Any]],
    *,
    board_summaries: list[dict[str, Any]],
    failures: list[dict[str, Any]],
) -> dict[str, Any]:
    label_counts: dict[str, dict[str, Any]] = {}
    normalized_label_counts: dict[str, dict[str, Any]] = {}
    signature_counts: Counter[str] = Counter()
    group_counts: Counter[str] = Counter()
    ui_type_counts: Counter[str] = Counter()
    api_type_counts: Counter[str] = Counter()
    question_counts_per_job: list[int] = []
    required_counts_per_job: list[int] = []

    for job in job_records:
        question_counts_per_job.append(int(job["question_count"]))
        required_counts_per_job.append(int(job["required_question_count"]))
        for key, value in (job.get("group_counts") or {}).items():
            group_counts[key] += int(value)
        for key, value in (job.get("ui_type_counts") or {}).items():
            ui_type_counts[key] += int(value)
        for key, value in (job.get("api_type_counts") or {}).items():
            api_type_counts[key] += int(value)
        for question in job.get("questions") or []:
            label = question["label"]
            normalized_label = question["normalized_label"]
            signature = _question_signature(question)
            signature_counts[signature] += 1

            label_entry = label_counts.setdefault(
                label,
                {
                    "label": label,
                    "count": 0,
                    "required_count": 0,
                    "groups": Counter(),
                    "signatures": Counter(),
                    "sample_jobs": [],
                },
            )
            label_entry["count"] += 1
            label_entry["required_count"] += 1 if question["required"] else 0
            label_entry["groups"][question["group"]] += 1
            label_entry["signatures"][signature] += 1
            if len(label_entry["sample_jobs"]) < 5:
                label_entry["sample_jobs"].append(
                    {
                        "company": job["company"],
                        "title": job["title"],
                        "job_url": job["job_url"],
                    }
                )

            normalized_entry = normalized_label_counts.setdefault(
                normalized_label,
                {
                    "normalized_label": normalized_label,
                    "count": 0,
                    "required_count": 0,
                    "labels": Counter(),
                    "groups": Counter(),
                    "signatures": Counter(),
                },
            )
            normalized_entry["count"] += 1
            normalized_entry["required_count"] += 1 if question["required"] else 0
            normalized_entry["labels"][label] += 1
            normalized_entry["groups"][question["group"]] += 1
            normalized_entry["signatures"][signature] += 1

    def _top_entries(raw_entries: dict[str, dict[str, Any]], *, key_name: str) -> list[dict[str, Any]]:
        ordered = sorted(raw_entries.values(), key=lambda item: (-int(item["count"]), item[key_name]))
        out: list[dict[str, Any]] = []
        for item in ordered[:50]:
            out.append(
                {
                    key_name: item[key_name],
                    "count": int(item["count"]),
                    "required_count": int(item["required_count"]),
                    "groups": dict(item["groups"]),
                    "signatures": dict(item["signatures"]),
                    "labels": dict(item["labels"]) if "labels" in item else None,
                    "sample_jobs": item.get("sample_jobs"),
                }
            )
        return out

    question_stats = {
        "jobs_with_questions": len(question_counts_per_job),
        "questions_per_job_min": min(question_counts_per_job) if question_counts_per_job else 0,
        "questions_per_job_max": max(question_counts_per_job) if question_counts_per_job else 0,
        "questions_per_job_avg": (
            round(sum(question_counts_per_job) / len(question_counts_per_job), 2)
            if question_counts_per_job
            else 0.0
        ),
        "questions_per_job_median": (
            statistics.median(question_counts_per_job) if question_counts_per_job else 0
        ),
        "required_questions_per_job_avg": (
            round(sum(required_counts_per_job) / len(required_counts_per_job), 2)
            if required_counts_per_job
            else 0.0
        ),
    }

    return {
        "generated_at": utc_now(),
        "boards_surveyed": len(board_summaries),
        "jobs_surveyed": len(job_records),
        "job_failures": len(failures),
        "question_stats": question_stats,
        "group_counts": dict(group_counts),
        "ui_type_counts": dict(ui_type_counts),
        "api_type_counts": dict(api_type_counts),
        "top_question_labels": _top_entries(label_counts, key_name="label"),
        "top_normalized_questions": _top_entries(
            normalized_label_counts,
            key_name="normalized_label",
        ),
        "field_signatures": [
            {"signature": signature, "count": count}
            for signature, count in signature_counts.most_common(25)
        ],
        "board_summaries": board_summaries,
        "failures": failures,
        "jobs": job_records,
    }


def run_greenhouse_survey(
    *,
    config_path: Path,
    output_path: Path,
    company_filters: list[str] | None = None,
    max_jobs_per_board: int = 0,
    max_workers: int = 6,
) -> dict[str, Any]:
    targets = _load_greenhouse_targets(config_path, company_filters=company_filters)
    board_jobs: list[dict[str, Any]] = []
    board_summaries: list[dict[str, Any]] = []
    failures: list[dict[str, Any]] = []

    for target in targets:
        jobs = _fetch_board_jobs(board=target["board"], timeout_seconds=target["timeout_seconds"])
        if max_jobs_per_board > 0:
            jobs = jobs[:max_jobs_per_board]
        board_jobs.extend(
            {
                "company": target["company"],
                "board": target["board"],
                "timeout_seconds": target["timeout_seconds"],
                "job_id": job["job_id"],
                "job_url": job["url"],
                "title": job["title"],
                "location": job["location"],
            }
            for job in jobs
        )
        board_summaries.append(
            {
                "company": target["company"],
                "board": target["board"],
                "jobs_discovered": len(jobs),
                "jobs_surveyed": 0,
                "jobs_failed": 0,
                "top_labels": [],
            }
        )

    board_summary_index = {item["board"]: item for item in board_summaries}

    job_records: list[dict[str, Any]] = []
    with ThreadPoolExecutor(max_workers=max(1, max_workers)) as executor:
        future_map = {
            executor.submit(
                _survey_job,
                company=job["company"],
                board=job["board"],
                job_id=job["job_id"],
                url=job["job_url"],
                timeout_seconds=job["timeout_seconds"],
            ): job
            for job in board_jobs
        }
        for future in as_completed(future_map):
            job = future_map[future]
            board_summary = board_summary_index[job["board"]]
            try:
                record = future.result()
            except Exception as exc:  # noqa: BLE001
                board_summary["jobs_failed"] += 1
                failures.append(
                    {
                        "company": job["company"],
                        "board": job["board"],
                        "job_id": job["job_id"],
                        "job_url": job["job_url"],
                        "title": job["title"],
                        "error": str(exc),
                    }
                )
                continue
            board_summary["jobs_surveyed"] += 1
            job_records.append(record)

    for board_summary in board_summaries:
        counter: Counter[str] = Counter()
        for job in job_records:
            if job["board"] != board_summary["board"]:
                continue
            for question in job["questions"]:
                counter[question["label"]] += 1
        board_summary["top_labels"] = [
            {"label": label, "count": count} for label, count in counter.most_common(10)
        ]

    report = summarize_survey(job_records, board_summaries=board_summaries, failures=failures)
    report["config_path"] = str(config_path)
    report["max_jobs_per_board"] = max_jobs_per_board
    report["max_workers"] = max_workers
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")
    return report


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Survey configured Greenhouse job application schemas and aggregate their fields/questions."
    )
    parser.add_argument(
        "--config",
        default="companies.yaml",
        help="Path to the companies YAML file. Default: companies.yaml",
    )
    parser.add_argument(
        "--output",
        default=".state/greenhouse_form_survey.json",
        help="Path to write the survey JSON report. Default: .state/greenhouse_form_survey.json",
    )
    parser.add_argument(
        "--company",
        action="append",
        default=[],
        help="Optional company name filter. Repeat to include multiple companies.",
    )
    parser.add_argument(
        "--max-jobs-per-board",
        type=int,
        default=0,
        help="Optional cap per board. Use 0 to survey all discovered jobs.",
    )
    parser.add_argument(
        "--max-workers",
        type=int,
        default=6,
        help="Maximum concurrent schema fetches. Default: 6",
    )
    parser.add_argument(
        "--indent",
        type=int,
        default=2,
        help="Indent level for stdout JSON summary. Default: 2",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        report = run_greenhouse_survey(
            config_path=Path(args.config),
            output_path=Path(args.output),
            company_filters=args.company,
            max_jobs_per_board=max(0, int(args.max_jobs_per_board)),
            max_workers=max(1, int(args.max_workers)),
        )
    except Exception as exc:  # noqa: BLE001
        print(f"error: {exc}", file=sys.stderr)
        return 1

    summary = {
        "output_path": str(Path(args.output)),
        "generated_at": report["generated_at"],
        "boards_surveyed": report["boards_surveyed"],
        "jobs_surveyed": report["jobs_surveyed"],
        "job_failures": report["job_failures"],
        "question_stats": report["question_stats"],
        "group_counts": report["group_counts"],
        "ui_type_counts": report["ui_type_counts"],
        "api_type_counts": report["api_type_counts"],
        "top_question_labels": report["top_question_labels"][:15],
        "field_signatures": report["field_signatures"][:15],
    }
    print(json.dumps(summary, indent=args.indent, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
