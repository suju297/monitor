#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
from copy import deepcopy
from datetime import datetime, timezone
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build crawl-derived gold cases for policy routing.")
    parser.add_argument("--probe-report", required=True, help="Probe report JSON from go/cmd/policyprobe.")
    parser.add_argument("--out", required=True, help="Output JSON file.")
    return parser.parse_args()


def build_case(case_id: str, kind: str, payload: dict, gold_plan: list[str], case_probe: dict) -> dict:
    return {
        "id": case_id,
        "kind": kind,
        "payload": payload,
        "gold_plan": gold_plan,
        "company_name": case_probe["company_name"],
        "primary_source": case_probe["primary_source"],
        "allowed_plan": case_probe["allowed_plan"],
        "notes": case_probe.get("notes", ""),
        "probes": case_probe["probes"],
    }


def main() -> int:
    args = parse_args()
    probe_report = json.loads(Path(args.probe_report).read_text())

    gold_cases: list[dict] = []
    for case_probe in probe_report.get("cases", []):
        allowed_plan = list(case_probe.get("allowed_plan", []))
        gold_plan = list(case_probe.get("suggested_gold_plan", []))
        payload_company = deepcopy(case_probe.get("payload_company", {}))
        if not allowed_plan or not gold_plan:
            continue

        default_payload = {
            "company": payload_company,
            "prior_status": None,
            "allowed_plan": allowed_plan,
        }
        gold_cases.append(
            build_case(
                case_id=f"{case_probe['id']}:default",
                kind="default",
                payload=default_payload,
                gold_plan=gold_plan,
                case_probe=case_probe,
            )
        )

        blocked_primary = allowed_plan[0]
        if len(gold_plan) > 1 and blocked_primary in gold_plan:
            blocked_plan = [source for source in gold_plan if source != blocked_primary] + [blocked_primary]
            blocked_payload = {
                "company": payload_company,
                "prior_status": {
                    "status": "blocked",
                    "selected_source": blocked_primary,
                    "attempted_sources": [blocked_primary],
                    "message": "Primary source was previously blocked in a real crawl benchmark case.",
                    "updated_at": "",
                },
                "allowed_plan": allowed_plan,
            }
            gold_cases.append(
                build_case(
                    case_id=f"{case_probe['id']}:blocked-primary",
                    kind="blocked-primary",
                    payload=blocked_payload,
                    gold_plan=blocked_plan,
                    case_probe=case_probe,
                )
            )

    out = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "task": "source-plan routing",
        "source": str(Path(args.probe_report)),
        "cases": gold_cases,
    }
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(out, indent=2) + "\n")
    print(out_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
