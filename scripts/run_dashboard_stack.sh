#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

APP_HOST="${DASHBOARD_HOST:-127.0.0.1}"
APP_PORT="${DASHBOARD_PORT:-8765}"
FRONTEND_PORT="${DASHBOARD_FRONTEND_PORT:-5173}"
OBSERVER_HOST="${DASHBOARD_OBSERVER_HOST:-127.0.0.1}"
OBSERVER_PORT="${DASHBOARD_OBSERVER_PORT:-8776}"
APP_CONFIG="${DASHBOARD_CONFIG:-companies.yaml}"
APP_STATE_FILE="${DASHBOARD_STATE_FILE:-.state/openings_state.json}"
APP_REPORT_FILE="${DASHBOARD_REPORT_FILE:-.state/last_run_report.json}"
APP_SCHEDULE_FILE="${DASHBOARD_SCHEDULE_FILE:-.state/crawl_schedule.json}"
APP_DOTENV="${DASHBOARD_DOTENV:-.env}"
ALERT_ON_BLOCKED="${DASHBOARD_ALERT_ON_BLOCKED:-1}"

usage() {
  cat <<'EOF'
Usage:
  ./scripts/run_dashboard_stack.sh start [-- extra dashboard.py args]
  ./scripts/run_dashboard_stack.sh stop
  ./scripts/run_dashboard_stack.sh restart [-- extra dashboard.py args]
  ./scripts/run_dashboard_stack.sh status

Env overrides:
  DASHBOARD_HOST
  DASHBOARD_PORT
  DASHBOARD_FRONTEND_PORT
  DASHBOARD_OBSERVER_HOST
  DASHBOARD_OBSERVER_PORT
  DASHBOARD_CONFIG
  DASHBOARD_STATE_FILE
  DASHBOARD_REPORT_FILE
  DASHBOARD_SCHEDULE_FILE
  DASHBOARD_DOTENV
  DASHBOARD_ALERT_ON_BLOCKED   (1=true, 0=false)
EOF
}

command_for_pid() {
  ps -p "$1" -o command= 2>/dev/null || true
}

listener_pids() {
  lsof -nP "-iTCP:$1" -sTCP:LISTEN -t 2>/dev/null || true
}

is_alive() {
  kill -0 "$1" 2>/dev/null
}

terminate_pid() {
  local pid="$1"
  kill -TERM "$pid" 2>/dev/null || return 0
  local end=$((SECONDS + 4))
  while (( SECONDS < end )); do
    if ! is_alive "$pid"; then
      return 0
    fi
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
}

looks_like_backend() {
  local cmd
  cmd="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  [[ "$cmd" == *"dashboard.py"* ]] && return 0
  [[ "$cmd" == *"go -c go run ./cmd/dashboard"* ]] && return 0
  [[ "$cmd" == *"/go-build/"*"/dashboard "* && "$cmd" == *"--state-file"* && "$cmd" == *"--report-file"* ]] && return 0
  return 1
}

looks_like_frontend() {
  local cmd
  cmd="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  [[ "$cmd" == *"pnpm --dir web dev"* ]] && return 0
  [[ "$cmd" == *"/vite/bin/vite.js"* ]] && return 0
  return 1
}

looks_like_observer() {
  local cmd
  cmd="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  [[ "$cmd" == *"career_monitor.greenhouse_observer_api"* ]] && return 0
  return 1
}

stop_owned_listeners() {
  local port="$1"
  local role="$2"
  local pid cmd
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    cmd="$(command_for_pid "$pid")"
    if [[ "$role" == "backend" ]]; then
      looks_like_backend "$cmd" || continue
    elif [[ "$role" == "observer" ]]; then
      looks_like_observer "$cmd" || continue
    else
      looks_like_frontend "$cmd" || continue
    fi
    printf 'Stopping %s listener pid=%s\n' "$role" "$pid"
    terminate_pid "$pid"
  done < <(listener_pids "$port")
}

stop_parent_wrappers() {
  local pid cmd
  while read -r pid cmd; do
    [[ -n "$pid" ]] || continue
    if [[ "$cmd" == *"dashboard.py"* && "$cmd" == *"--port $APP_PORT"* ]]; then
      printf 'Stopping dashboard wrapper pid=%s\n' "$pid"
      terminate_pid "$pid"
    fi
  done < <(ps -axo pid=,command=)
}

status() {
  local backend_ok frontend_ok
  backend_ok="down"
  frontend_ok="down"
  local observer_ok="down"
  if curl -sf "http://$APP_HOST:$APP_PORT/api/health" >/dev/null 2>&1; then
    backend_ok="up"
  fi
  if curl -sf "http://127.0.0.1:$FRONTEND_PORT/monitor" >/dev/null 2>&1; then
    frontend_ok="up"
  fi
  if curl -sf "http://$OBSERVER_HOST:$OBSERVER_PORT/api/health" >/dev/null 2>&1; then
    observer_ok="up"
  fi

  printf 'Backend:  %s (%s)\n' "$backend_ok" "http://$APP_HOST:$APP_PORT"
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    printf '  pid=%s %s\n' "$pid" "$(command_for_pid "$pid")"
  done < <(listener_pids "$APP_PORT")

  printf 'Observer: %s (%s)\n' "$observer_ok" "http://$OBSERVER_HOST:$OBSERVER_PORT"
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    printf '  pid=%s %s\n' "$pid" "$(command_for_pid "$pid")"
  done < <(listener_pids "$OBSERVER_PORT")

  printf 'Frontend: %s (%s)\n' "$frontend_ok" "http://127.0.0.1:$FRONTEND_PORT/monitor"
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    printf '  pid=%s %s\n' "$pid" "$(command_for_pid "$pid")"
  done < <(listener_pids "$FRONTEND_PORT")
}

stop_stack() {
  stop_parent_wrappers
  stop_owned_listeners "$FRONTEND_PORT" "frontend"
  stop_owned_listeners "$OBSERVER_PORT" "observer"
  stop_owned_listeners "$APP_PORT" "backend"
}

start_stack() {
  mkdir -p .state
  printf 'Checking for existing managed stack on backend :%s, observer :%s, and frontend :%s\n' "$APP_PORT" "$OBSERVER_PORT" "$FRONTEND_PORT"
  stop_stack
  local args=(
    dashboard.py
    --fresh
    --host "$APP_HOST"
    --port "$APP_PORT"
    --config "$APP_CONFIG"
    --state-file "$APP_STATE_FILE"
    --report-file "$APP_REPORT_FILE"
    --schedule-file "$APP_SCHEDULE_FILE"
    --dotenv "$APP_DOTENV"
  )
  if [[ "$ALERT_ON_BLOCKED" == "1" || "$ALERT_ON_BLOCKED" == "true" || "$ALERT_ON_BLOCKED" == "yes" ]]; then
    args+=(--alert-on-blocked)
  fi
  if (($# > 0)); then
    args+=("$@")
  fi
  exec python3 "${args[@]}"
}

subcommand="${1:-start}"
if (($# > 0)); then
  shift
fi

case "$subcommand" in
  start)
    start_stack "$@"
    ;;
  stop)
    stop_stack
    ;;
  restart)
    stop_stack
    start_stack "$@"
    ;;
  status)
    status
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    usage
    exit 1
    ;;
esac
