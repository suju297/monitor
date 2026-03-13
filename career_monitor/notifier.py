from __future__ import annotations

import os
import re
import smtplib
import ssl
from datetime import datetime
from email.message import EmailMessage

from .models import CrawlOutcome, Job
from .tracking import grouped_jobs
from .utils import parse_bool_env


def build_email_content(new_jobs: list[Job], blocked: list[CrawlOutcome]) -> tuple[str, str]:
    subject = (
        f"[Career Monitor] {len(new_jobs)} new opening(s)"
        f" | {len(blocked)} blocked target(s)"
        f" - {datetime.now().strftime('%Y-%m-%d %H:%M')}"
    )
    lines = [
        "Career monitor update.",
        "",
        f"Total new openings: {len(new_jobs)}",
        f"Blocked targets: {len(blocked)}",
        "",
    ]

    if new_jobs:
        lines.append("New openings")
        lines.append("")
        for company, jobs in grouped_jobs(new_jobs).items():
            lines.append(f"{company} ({len(jobs)})")
            for job in jobs:
                extras = [entry for entry in [job.team, job.location] if entry]
                suffix = f" [{', '.join(extras)}]" if extras else ""
                lines.append(f"- {job.title}{suffix}")
                lines.append(f"  {job.url}")
            lines.append("")

    if blocked:
        lines.append("Blocked targets")
        lines.append("")
        for outcome in sorted(blocked, key=lambda item: item.company.lower()):
            chain = " -> ".join(outcome.attempted_sources)
            lines.append(f"- {outcome.company} ({chain})")
            lines.append(f"  {outcome.message}")
        lines.append("")

    body = "\n".join(lines).strip() + "\n"
    return subject, body


def send_email(subject: str, body: str) -> None:
    smtp_host = os.getenv("SMTP_HOST", "").strip()
    smtp_port = int(os.getenv("SMTP_PORT", "587").strip())
    smtp_user = os.getenv("SMTP_USER", "").strip()
    smtp_pass = os.getenv("SMTP_PASS", "").strip()
    email_from = os.getenv("EMAIL_FROM", "").strip() or smtp_user
    raw_email_to = os.getenv("EMAIL_TO", "").strip()
    use_tls = parse_bool_env("SMTP_USE_TLS", True)

    recipients = [entry.strip() for entry in re.split(r"[;,]", raw_email_to) if entry.strip()]
    missing = []
    if not smtp_host:
        missing.append("SMTP_HOST")
    if not smtp_user:
        missing.append("SMTP_USER")
    if not smtp_pass:
        missing.append("SMTP_PASS")
    if not email_from:
        missing.append("EMAIL_FROM")
    if not recipients:
        missing.append("EMAIL_TO")
    if missing:
        raise RuntimeError(f"Missing email configuration: {', '.join(missing)}")

    message = EmailMessage()
    message["Subject"] = subject
    message["From"] = email_from
    message["To"] = ", ".join(recipients)
    message.set_content(body)

    if use_tls:
        with smtplib.SMTP(smtp_host, smtp_port) as client:
            client.starttls(context=ssl.create_default_context())
            client.login(smtp_user, smtp_pass)
            client.send_message(message)
        return
    with smtplib.SMTP_SSL(smtp_host, smtp_port, context=ssl.create_default_context()) as client:
        client.login(smtp_user, smtp_pass)
        client.send_message(message)

