from __future__ import annotations

import argparse
import logging
from pathlib import Path

from ..config import load_companies
from ..constants import DEFAULT_WORKERS
from ..local_paths import prefer_local_file
from ..notifier import build_email_content, send_email
from ..orchestrator import crawl_companies
from ..reporting import write_run_report
from ..state import load_state, save_state, update_last_run
from ..tracking import apply_outcomes_to_state
from ..utils import load_dotenv


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Career monitor v2: adapter-based crawler swarm with fallback orchestration and email alerts."
        )
    )
    parser.add_argument("--config", default="companies.yaml", help="Path to YAML config file.")
    parser.add_argument(
        "--state-file",
        default=".state/openings_state.json",
        help="Path to persistent state JSON.",
    )
    parser.add_argument(
        "--report-file",
        default=".state/last_run_report.json",
        help="Path to JSON run report.",
    )
    parser.add_argument("--dotenv", default=".env", help="Path to .env file to load.")
    parser.add_argument(
        "--workers",
        type=int,
        default=DEFAULT_WORKERS,
        help=f"Concurrent crawler workers. Default: {DEFAULT_WORKERS}.",
    )
    parser.add_argument(
        "--baseline",
        action="store_true",
        help="Mark current listings as seen, but skip email.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Do not send email. Print generated email content.",
    )
    parser.add_argument(
        "--alert-on-blocked",
        action="store_true",
        help="Send email even if only blocked targets were detected.",
    )
    parser.add_argument("--verbose", action="store_true", help="Enable verbose logging.")
    return parser.parse_args()


def run(args: argparse.Namespace) -> int:
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
    )
    load_dotenv(Path(args.dotenv))

    try:
        companies = load_companies(prefer_local_file(args.config, "companies.yaml"))
    except Exception as exc:  # noqa: BLE001
        logging.error("Failed to load config: %s", exc)
        return 1

    if not companies:
        logging.error("No enabled companies found in config.")
        return 1

    state_path = Path(args.state_file)
    report_path = Path(args.report_file)
    state = load_state(state_path)
    prior_status = state.get("company_status", {})
    outcomes = crawl_companies(companies, workers=args.workers, prior_company_status=prior_status)
    new_jobs, blocked_outcomes, status_lines = apply_outcomes_to_state(outcomes, state)
    update_last_run(state)
    save_state(state_path, state)

    write_run_report(
        report_path=report_path,
        outcomes=outcomes,
        new_jobs=new_jobs,
        blocked_count=len(blocked_outcomes),
        dry_run=bool(args.dry_run),
        baseline=bool(args.baseline),
    )

    for line in status_lines:
        logging.info(line)

    if args.baseline:
        logging.info(
            "Baseline mode enabled. Marked %d opening(s) as seen and skipped email.",
            len(new_jobs),
        )
        return 0

    should_send = bool(new_jobs) or (args.alert_on_blocked and bool(blocked_outcomes))
    if not should_send:
        logging.info("No new openings detected.")
        if blocked_outcomes:
            logging.info(
                "%d target(s) are blocked. Use --alert-on-blocked to email blocked summary.",
                len(blocked_outcomes),
            )
        return 0

    subject, body = build_email_content(new_jobs, blocked_outcomes)
    if args.dry_run:
        logging.info("Dry run enabled. Email was not sent.")
        print("\n--- EMAIL SUBJECT ---")
        print(subject)
        print("\n--- EMAIL BODY ---")
        print(body)
        return 0

    try:
        send_email(subject, body)
    except Exception as exc:  # noqa: BLE001
        logging.error("Failed to send email: %s", exc)
        return 1

    logging.info(
        "Sent alert email for %d new opening(s), %d blocked target(s).",
        len(new_jobs),
        len(blocked_outcomes),
    )
    return 0


def main() -> int:
    return run(parse_args())
