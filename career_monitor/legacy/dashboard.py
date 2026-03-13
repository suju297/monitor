from __future__ import annotations

import argparse
import json
import logging
import subprocess
import threading
from datetime import datetime, timezone
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

from ..local_paths import prefer_local_file
from ..utils import load_dotenv


def utc_now() -> str:
    return datetime.now(tz=timezone.utc).isoformat(timespec="seconds")


def read_json_file(path: Path, default: Any) -> Any:
    if not path.exists():
        return default
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception:  # noqa: BLE001
        return default


def iso_to_local(iso_value: str | None) -> str:
    if not iso_value:
        return "-"
    try:
        parsed = datetime.fromisoformat(iso_value.replace("Z", "+00:00"))
        return parsed.astimezone().strftime("%Y-%m-%d %H:%M:%S")
    except Exception:  # noqa: BLE001
        return iso_value


class MonitorRunner:
    def __init__(
        self,
        cwd: Path,
        config_path: Path,
        state_path: Path,
        report_path: Path,
        dotenv_path: Path,
        workers: int,
        alert_on_blocked: bool,
    ) -> None:
        self.cwd = cwd
        self.config_path = config_path
        self.state_path = state_path
        self.report_path = report_path
        self.dotenv_path = dotenv_path
        self.workers = workers
        self.alert_on_blocked = alert_on_blocked

        self._lock = threading.Lock()
        self._running = False
        self._status: dict[str, Any] = {
            "running": False,
            "last_start": None,
            "last_end": None,
            "last_exit_code": None,
            "last_mode": None,
            "last_stdout": "",
            "last_stderr": "",
            "last_error": "",
        }

    def snapshot(self) -> dict[str, Any]:
        with self._lock:
            return dict(self._status)

    def trigger(self, dry_run: bool) -> tuple[bool, str]:
        with self._lock:
            if self._running:
                return False, "Tracker run already in progress."
            self._running = True
            self._status["running"] = True
            self._status["last_start"] = utc_now()
            self._status["last_mode"] = "dry-run" if dry_run else "live-run"
            self._status["last_stdout"] = ""
            self._status["last_stderr"] = ""
            self._status["last_error"] = ""

        thread = threading.Thread(target=self._run_worker, args=(dry_run,), daemon=True)
        thread.start()
        return True, "Run started."

    def _run_worker(self, dry_run: bool) -> None:
        cmd = [
            "uv",
            "run",
            "python",
            "-m",
            "career_monitor.legacy.cli",
            "--config",
            str(self.config_path),
            "--state-file",
            str(self.state_path),
            "--report-file",
            str(self.report_path),
            "--dotenv",
            str(self.dotenv_path),
            "--workers",
            str(self.workers),
        ]
        if self.alert_on_blocked:
            cmd.append("--alert-on-blocked")
        if dry_run:
            cmd.append("--dry-run")

        try:
            proc = subprocess.run(  # noqa: S603,S607
                cmd,
                cwd=str(self.cwd),
                capture_output=True,
                text=True,
                check=False,
            )
            with self._lock:
                self._status["last_exit_code"] = proc.returncode
                self._status["last_stdout"] = (proc.stdout or "")[-20000:]
                self._status["last_stderr"] = (proc.stderr or "")[-20000:]
        except Exception as exc:  # noqa: BLE001
            with self._lock:
                self._status["last_error"] = f"{type(exc).__name__}: {exc}"
                self._status["last_exit_code"] = -1
        finally:
            with self._lock:
                self._running = False
                self._status["running"] = False
                self._status["last_end"] = utc_now()


class DashboardApp:
    def __init__(
        self,
        cwd: Path,
        state_path: Path,
        report_path: Path,
        runner: MonitorRunner,
    ) -> None:
        self.cwd = cwd
        self.state_path = state_path
        self.report_path = report_path
        self.runner = runner

    def build_overview(self) -> dict[str, Any]:
        state = read_json_file(
            self.state_path,
            {"seen": {}, "company_status": {}, "blocked_events": {}, "last_run": None},
        )
        report = read_json_file(self.report_path, {})

        seen = state.get("seen", {})
        company_status = state.get("company_status", {})
        blocked_events = state.get("blocked_events", {})
        report_summary = report.get("summary", {})
        report_new_jobs = report.get("new_jobs", [])

        company_names = sorted(
            set(seen.keys()) | set(company_status.keys()) | set(blocked_events.keys()),
            key=lambda value: value.lower(),
        )

        companies: list[dict[str, Any]] = []
        status_counts = {"ok": 0, "blocked": 0, "error": 0, "unknown": 0}
        for name in company_names:
            status_payload = company_status.get(name, {})
            status = str(status_payload.get("status", "unknown") or "unknown").lower()
            if status not in status_counts:
                status = "unknown"
            status_counts[status] += 1
            companies.append(
                {
                    "name": name,
                    "status": status,
                    "selected_source": status_payload.get("selected_source") or "-",
                    "attempted_sources": status_payload.get("attempted_sources") or [],
                    "message": status_payload.get("message") or "",
                    "updated_at": status_payload.get("updated_at"),
                    "updated_at_local": iso_to_local(status_payload.get("updated_at")),
                    "seen_jobs": len(seen.get(name, {})),
                    "blocked_events": len(blocked_events.get(name, [])),
                }
            )

        blocked_recent: list[dict[str, Any]] = []
        for company, events in blocked_events.items():
            if not isinstance(events, list):
                continue
            for event in events:
                if not isinstance(event, dict):
                    continue
                blocked_recent.append(
                    {
                        "company": company,
                        "at": event.get("at"),
                        "at_local": iso_to_local(event.get("at")),
                        "message": event.get("message") or "",
                        "attempted_sources": event.get("attempted_sources") or [],
                    }
                )
        blocked_recent.sort(key=lambda item: item.get("at") or "", reverse=True)
        blocked_recent = blocked_recent[:60]

        new_jobs = report_new_jobs[:200] if isinstance(report_new_jobs, list) else []

        summary = {
            "generated_at": utc_now(),
            "last_run": state.get("last_run"),
            "last_run_local": iso_to_local(state.get("last_run")),
            "companies_total": len(company_names),
            "status_counts": status_counts,
            "total_seen_jobs": sum(len(entries) for entries in seen.values() if isinstance(entries, dict)),
            "new_jobs_last_report": int(report_summary.get("new_jobs_count", len(new_jobs)) or 0),
            "blocked_last_report": int(report_summary.get("blocked_count", 0) or 0),
        }

        return {
            "summary": summary,
            "companies": companies,
            "blocked_recent": blocked_recent,
            "new_jobs": new_jobs,
            "runner": self.runner.snapshot(),
            "paths": {
                "state_file": str(self.state_path),
                "report_file": str(self.report_path),
                "workspace": str(self.cwd),
            },
        }

    def run_monitor(self, dry_run: bool) -> tuple[bool, str]:
        return self.runner.trigger(dry_run=dry_run)


def dashboard_html() -> str:
    return """<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Career Monitor Dashboard</title>
  <style>
    :root {
      --bg: #f7f8f4;
      --panel: #ffffff;
      --text: #1d2428;
      --muted: #5d6770;
      --line: #d8ddde;
      --accent: #0f766e;
      --accent-2: #b45309;
      --ok: #15803d;
      --blocked: #b91c1c;
      --error: #9a3412;
      --unknown: #475569;
      --shadow: 0 18px 45px rgba(22, 28, 45, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      color: var(--text);
      background:
        radial-gradient(circle at 12% -15%, #b6efe7 0%, transparent 42%),
        radial-gradient(circle at 88% -12%, #ffe0b8 0%, transparent 38%),
        var(--bg);
    }
    .wrap {
      max-width: 1280px;
      margin: 0 auto;
      padding: 22px 18px 30px;
      animation: rise 300ms ease-out;
    }
    @keyframes rise {
      from { opacity: 0; transform: translateY(8px); }
      to { opacity: 1; transform: translateY(0); }
    }
    .hero {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      align-items: center;
      justify-content: space-between;
      margin-bottom: 14px;
    }
    h1 { margin: 0; font-size: clamp(1.2rem, 3vw, 1.8rem); letter-spacing: 0.2px; }
    .sub { color: var(--muted); font-size: 0.92rem; margin-top: 3px; }
    .controls { display: flex; gap: 8px; flex-wrap: wrap; }
    button {
      border: 0;
      padding: 10px 14px;
      border-radius: 10px;
      font-weight: 600;
      cursor: pointer;
      transition: transform 0.15s ease, filter 0.2s ease;
    }
    button:hover { transform: translateY(-1px); filter: brightness(0.98); }
    .btn-main { background: var(--accent); color: #fff; }
    .btn-warn { background: var(--accent-2); color: #fff; }
    .btn-ghost { background: #eef2f2; color: #20292c; }
    .cards {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 10px;
      margin-bottom: 12px;
    }
    .card {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 12px;
      padding: 12px;
      box-shadow: var(--shadow);
    }
    .k { color: var(--muted); font-size: 0.78rem; text-transform: uppercase; letter-spacing: 0.7px; }
    .v { font-size: 1.38rem; font-weight: 700; margin-top: 4px; }
    .grid {
      display: grid;
      grid-template-columns: 1.6fr 1fr;
      gap: 12px;
    }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 12px;
      box-shadow: var(--shadow);
      overflow: hidden;
    }
    .panel h2 {
      margin: 0;
      padding: 11px 12px;
      border-bottom: 1px solid var(--line);
      font-size: 1rem;
      background: linear-gradient(90deg, rgba(15,118,110,0.08), rgba(180,83,9,0.06));
    }
    .table-wrap { max-height: 540px; overflow: auto; }
    table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
    th, td { border-bottom: 1px solid #e9eeef; padding: 8px 10px; vertical-align: top; }
    th { position: sticky; top: 0; background: #f3f7f6; text-align: left; z-index: 2; }
    .pill {
      display: inline-block;
      font-size: 0.72rem;
      border-radius: 999px;
      padding: 2px 8px;
      font-weight: 600;
      letter-spacing: 0.3px;
    }
    .ok { background: #dff3e5; color: var(--ok); }
    .blocked { background: #ffe1df; color: var(--blocked); }
    .error { background: #ffe8df; color: var(--error); }
    .unknown { background: #e8eef4; color: var(--unknown); }
    .list { max-height: 260px; overflow: auto; padding: 8px 10px; }
    .item { padding: 8px 0; border-bottom: 1px solid #edf1f2; }
    .item:last-child { border-bottom: 0; }
    .muted { color: var(--muted); font-size: 0.84rem; }
    .code {
      white-space: pre-wrap;
      max-height: 220px;
      overflow: auto;
      margin: 0;
      padding: 10px;
      border-top: 1px solid var(--line);
      background: #11161b;
      color: #d4e1e8;
      font-family: "IBM Plex Mono", "SFMono-Regular", monospace;
      font-size: 0.78rem;
    }
    .split { display: grid; grid-template-columns: 1fr; gap: 12px; }
    @media (max-width: 1020px) {
      .grid { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="hero">
      <div>
        <h1>Career Monitor Dashboard</h1>
        <div class="sub" id="meta">Loading...</div>
      </div>
      <div class="controls">
        <button class="btn-main" id="btn-run">Run Monitor</button>
        <button class="btn-warn" id="btn-dry">Dry Run</button>
        <button class="btn-ghost" id="btn-refresh">Refresh</button>
      </div>
    </div>

    <div class="cards" id="cards"></div>

    <div class="grid">
      <div class="panel">
        <h2>Company Status</h2>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Company</th>
                <th>Status</th>
                <th>Source</th>
                <th>Seen</th>
                <th>Blocked</th>
                <th>Updated</th>
                <th>Notes</th>
              </tr>
            </thead>
            <tbody id="company-table"></tbody>
          </table>
        </div>
      </div>

      <div class="split">
        <div class="panel">
          <h2>Blocked Events</h2>
          <div class="list" id="blocked-list"></div>
        </div>
        <div class="panel">
          <h2>New Jobs (Latest Report)</h2>
          <div class="list" id="new-jobs-list"></div>
        </div>
        <div class="panel">
          <h2>Run Output</h2>
          <div class="list">
            <div class="item"><b>Status:</b> <span id="run-status">-</span></div>
            <div class="item"><b>Last Mode:</b> <span id="run-mode">-</span></div>
            <div class="item"><b>Last Exit:</b> <span id="run-exit">-</span></div>
            <div class="item"><b>Last Start:</b> <span id="run-start">-</span></div>
            <div class="item"><b>Last End:</b> <span id="run-end">-</span></div>
          </div>
          <pre class="code" id="run-log"></pre>
        </div>
      </div>
    </div>
  </div>

  <script>
    const $ = (id) => document.getElementById(id);
    const esc = (v) => String(v ?? "").replace(/[&<>\"']/g, (c) => ({ "&":"&amp;", "<":"&lt;", ">":"&gt;", '\"':"&quot;", "'":"&#039;" }[c]));
    const pill = (status) => `<span class="pill ${esc(status)}">${esc(status)}</span>`;

    function renderCards(summary) {
      const cards = [
        ["Companies", summary.companies_total ?? 0],
        ["Seen Jobs", summary.total_seen_jobs ?? 0],
        ["New (Last Report)", summary.new_jobs_last_report ?? 0],
        ["Blocked (Last Report)", summary.blocked_last_report ?? 0],
        ["OK", summary.status_counts?.ok ?? 0],
        ["Blocked", summary.status_counts?.blocked ?? 0],
        ["Errors", summary.status_counts?.error ?? 0],
        ["Unknown", summary.status_counts?.unknown ?? 0],
      ];
      $("cards").innerHTML = cards.map(([k,v]) => `<div class="card"><div class="k">${esc(k)}</div><div class="v">${esc(v)}</div></div>`).join("");
    }

    function renderCompanies(companies) {
      $("company-table").innerHTML = companies.map((c) => `
        <tr>
          <td>${esc(c.name)}</td>
          <td>${pill(c.status)}</td>
          <td>${esc(c.selected_source || "-")}</td>
          <td>${esc(c.seen_jobs || 0)}</td>
          <td>${esc(c.blocked_events || 0)}</td>
          <td>${esc(c.updated_at_local || "-")}</td>
          <td class="muted">${esc(c.message || "").slice(0, 140)}</td>
        </tr>
      `).join("");
    }

    function renderBlocked(items) {
      if (!items.length) {
        $("blocked-list").innerHTML = `<div class="muted">No blocked events recorded.</div>`;
        return;
      }
      $("blocked-list").innerHTML = items.map((i) => `
        <div class="item">
          <div><b>${esc(i.company)}</b> <span class="muted">(${esc(i.at_local || "-")})</span></div>
          <div class="muted">${esc((i.attempted_sources || []).join(" -> "))}</div>
          <div>${esc(i.message || "")}</div>
        </div>
      `).join("");
    }

    function renderNewJobs(items) {
      if (!items.length) {
        $("new-jobs-list").innerHTML = `<div class="muted">No new jobs in latest report.</div>`;
        return;
      }
      $("new-jobs-list").innerHTML = items.slice(0, 80).map((j) => `
        <div class="item">
          <div><b>${esc(j.company || "-")}</b></div>
          <div>${esc(j.title || "-")}</div>
          <div class="muted">${esc(j.location || "")}</div>
          <div><a href="${esc(j.url || "#")}" target="_blank" rel="noopener noreferrer">${esc(j.url || "")}</a></div>
        </div>
      `).join("");
    }

    function renderRunner(r) {
      $("run-status").textContent = r.running ? "Running" : "Idle";
      $("run-mode").textContent = r.last_mode || "-";
      $("run-exit").textContent = r.last_exit_code ?? "-";
      $("run-start").textContent = r.last_start || "-";
      $("run-end").textContent = r.last_end || "-";
      const log = [r.last_stderr || "", r.last_stdout || "", r.last_error || ""].filter(Boolean).join("\\n\\n");
      $("run-log").textContent = log || "No run output yet.";
    }

    async function fetchOverview() {
      const res = await fetch("/api/overview", { cache: "no-store" });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return res.json();
    }

    async function refresh() {
      try {
        const data = await fetchOverview();
        const summary = data.summary || {};
        renderCards(summary);
        renderCompanies(data.companies || []);
        renderBlocked(data.blocked_recent || []);
        renderNewJobs(data.new_jobs || []);
        renderRunner(data.runner || {});
        $("meta").textContent = `Last run: ${summary.last_run_local || "-"} | Generated: ${summary.generated_at || "-"}`;
      } catch (err) {
        $("meta").textContent = `Dashboard fetch error: ${String(err)}`;
      }
    }

    async function triggerRun(dryRun) {
      const res = await fetch("/api/run", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ dry_run: !!dryRun }),
      });
      const data = await res.json();
      if (!res.ok) {
        alert(data.message || "Run trigger failed");
        return;
      }
      await refresh();
    }

    $("btn-refresh").addEventListener("click", () => refresh());
    $("btn-run").addEventListener("click", () => triggerRun(false));
    $("btn-dry").addEventListener("click", () => triggerRun(true));

    refresh();
    setInterval(refresh, 15000);
  </script>
</body>
</html>
"""


class DashboardHandler(BaseHTTPRequestHandler):
    app: DashboardApp

    def log_message(self, format: str, *args: Any) -> None:  # noqa: A003
        logging.info("%s - %s", self.address_string(), format % args)

    def _send_json(self, payload: Any, status: int = HTTPStatus.OK) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _send_html(self, html: str) -> None:
        body = html.encode("utf-8")
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802
        parsed = urlparse(self.path)
        if parsed.path == "/":
            self._send_html(dashboard_html())
            return
        if parsed.path == "/api/overview":
            self._send_json(self.app.build_overview())
            return
        if parsed.path == "/api/run-status":
            self._send_json(self.app.runner.snapshot())
            return
        if parsed.path == "/api/health":
            self._send_json({"ok": True, "time": utc_now()})
            return
        self._send_json({"error": "Not found"}, status=HTTPStatus.NOT_FOUND)

    def do_POST(self) -> None:  # noqa: N802
        parsed = urlparse(self.path)
        if parsed.path != "/api/run":
            self._send_json({"error": "Not found"}, status=HTTPStatus.NOT_FOUND)
            return

        length = int(self.headers.get("Content-Length", "0") or 0)
        raw = self.rfile.read(length) if length > 0 else b"{}"
        try:
            payload = json.loads(raw.decode("utf-8") or "{}")
        except Exception:  # noqa: BLE001
            self._send_json({"ok": False, "message": "Invalid JSON payload"}, status=HTTPStatus.BAD_REQUEST)
            return

        dry_run = bool(payload.get("dry_run", False))
        ok, message = self.app.run_monitor(dry_run=dry_run)
        status_code = HTTPStatus.OK if ok else HTTPStatus.CONFLICT
        self._send_json({"ok": ok, "message": message}, status=status_code)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run local dashboard for Career Monitor.")
    parser.add_argument("--host", default="127.0.0.1", help="Host to bind.")
    parser.add_argument("--port", type=int, default=8765, help="Port to bind.")
    parser.add_argument("--config", default="companies.yaml", help="Tracker config file.")
    parser.add_argument("--state-file", default=".state/openings_state.json", help="Tracker state file.")
    parser.add_argument("--report-file", default=".state/last_run_report.json", help="Tracker report file.")
    parser.add_argument("--dotenv", default=".env", help="Dotenv file.")
    parser.add_argument("--workers", type=int, default=12, help="Workers for triggered monitor runs.")
    parser.add_argument(
        "--alert-on-blocked",
        action="store_true",
        help="Include --alert-on-blocked when dashboard triggers a run.",
    )
    parser.add_argument("--verbose", action="store_true", help="Enable verbose logging.")
    return parser.parse_args()


def run_dashboard(args: argparse.Namespace) -> int:
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
    )

    cwd = Path.cwd()
    load_dotenv(Path(args.dotenv))
    runner = MonitorRunner(
        cwd=cwd,
        config_path=prefer_local_file(args.config, "companies.yaml"),
        state_path=Path(args.state_file),
        report_path=Path(args.report_file),
        dotenv_path=Path(args.dotenv),
        workers=max(1, int(args.workers)),
        alert_on_blocked=bool(args.alert_on_blocked),
    )
    app = DashboardApp(
        cwd=cwd,
        state_path=Path(args.state_file),
        report_path=Path(args.report_file),
        runner=runner,
    )
    DashboardHandler.app = app

    server = ThreadingHTTPServer((args.host, args.port), DashboardHandler)
    logging.info("Dashboard running at http://%s:%s", args.host, args.port)
    logging.info("Press Ctrl+C to stop.")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logging.info("Dashboard stopped.")
    finally:
        server.server_close()
    return 0


def main() -> int:
    return run_dashboard(parse_args())
