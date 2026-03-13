#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p .state/bin .state

# Skip live runs until SMTP_PASS is configured to avoid consuming new jobs without alerts.
if grep -q '^SMTP_PASS=REPLACE_WITH_GMAIL_APP_PASSWORD$' .env 2>/dev/null; then
  printf '[%s] monitor skipped: SMTP_PASS still placeholder in .env\n' "$(date '+%Y-%m-%d %H:%M:%S')" >> .state/cron_monitor.log
  exit 0
fi

if [[ ! -x .state/bin/monitor ]]; then
  go -C go build -o .state/bin/monitor ./cmd/monitor
fi

./.state/bin/monitor --workers 10 --alert-on-blocked >> .state/cron_monitor.log 2>&1
