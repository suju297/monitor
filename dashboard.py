#!/usr/bin/env python3
from __future__ import annotations

import os
import shlex
import signal
import subprocess
import sys
import time
from pathlib import Path
from urllib.error import URLError
from urllib.request import urlopen


DEFAULT_BACKEND_HOST = "127.0.0.1"
DEFAULT_BACKEND_PORT = 8765
FRONTEND_HOST = "127.0.0.1"
FRONTEND_PORT = int(os.getenv("DASHBOARD_FRONTEND_PORT", "5173"))
DEFAULT_OBSERVER_HOST = os.getenv("DASHBOARD_OBSERVER_HOST", "127.0.0.1")
DEFAULT_OBSERVER_PORT = int(os.getenv("DASHBOARD_OBSERVER_PORT", "8776"))
DEFAULT_OBSERVER_PROFILE_FILE = os.getenv(
    "DASHBOARD_OBSERVER_PROFILE_FILE",
    ".state/greenhouse_observer_profile.json",
)
DEFAULT_OBSERVER_ANSWERS_FILE = os.getenv(
    "DASHBOARD_OBSERVER_ANSWERS_FILE",
    ".state/greenhouse_observer_answers.json",
)
PATH_FLAGS = {"--config", "--state-file", "--report-file", "--schedule-file", "--dotenv"}
SCRIPT_DIR = Path(__file__).resolve().parent
GO_DIR = SCRIPT_DIR / "go"
SHUTDOWN_SIGNAL: str | None = None
DEBUG_ENV_KEYS = {
    "VSCODE_INSPECTOR_OPTIONS",
    "NODE_INSPECT_RESUME_ON_START",
    "JS_DEBUG_MODE",
}


class ShutdownRequested(Exception):
    pass


def parse_backend_host_port(args: list[str]) -> tuple[str, int]:
    host = DEFAULT_BACKEND_HOST
    port = DEFAULT_BACKEND_PORT
    i = 0
    while i < len(args):
        current = args[i]
        if current == "--host" and i + 1 < len(args):
            host = args[i + 1]
            i += 2
            continue
        if current == "--port" and i + 1 < len(args):
            try:
                port = int(args[i + 1])
            except ValueError:
                pass
            i += 2
            continue
        if current.startswith("--host="):
            host = current.split("=", 1)[1] or host
        elif current.startswith("--port="):
            raw_port = current.split("=", 1)[1]
            try:
                port = int(raw_port)
            except ValueError:
                pass
        i += 1
    return host, port


def backend_health_url(host: str, port: int) -> str:
    return f"http://{host}:{port}/api/health"


def frontend_monitor_url() -> str:
    return f"http://{FRONTEND_HOST}:{FRONTEND_PORT}/monitor"


def observer_health_url(host: str, port: int) -> str:
    return f"http://{host}:{port}/api/health"


def install_signal_handlers() -> None:
    def handle_signal(signum: int, _frame: object) -> None:
        global SHUTDOWN_SIGNAL
        if SHUTDOWN_SIGNAL is None:
            try:
                SHUTDOWN_SIGNAL = signal.Signals(signum).name
            except ValueError:
                SHUTDOWN_SIGNAL = f"signal {signum}"
            print(f"Received {SHUTDOWN_SIGNAL}. Shutting down managed services...")

    for name in ("SIGINT", "SIGTERM", "SIGHUP"):
        signum = getattr(signal, name, None)
        if signum is not None:
            signal.signal(signum, handle_signal)


def check_shutdown_requested() -> None:
    if SHUTDOWN_SIGNAL is not None:
        raise ShutdownRequested(SHUTDOWN_SIGNAL)


def url_ready(url: str, timeout: float = 1.5) -> bool:
    try:
        with urlopen(url, timeout=timeout) as response:  # noqa: S310
            return 200 <= int(response.status) < 500
    except (URLError, TimeoutError, OSError, ValueError):
        return False


def wait_for_url(url: str, timeout_seconds: float) -> bool:
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        check_shutdown_requested()
        if url_ready(url):
            return True
        time.sleep(0.4)
    return False


def listener_pids(port: int) -> list[int]:
    result = subprocess.run(  # noqa: S603
        ["lsof", "-nP", f"-iTCP:{port}", "-sTCP:LISTEN", "-t"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode not in (0, 1):
        return []
    out: list[int] = []
    for line in result.stdout.splitlines():
        value = line.strip()
        if not value:
            continue
        try:
            out.append(int(value))
        except ValueError:
            continue
    return out


def command_for_pid(pid: int) -> str:
    result = subprocess.run(  # noqa: S603
        ["ps", "-p", str(pid), "-o", "command="],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        return ""
    return result.stdout.strip()


def is_pid_alive(pid: int) -> bool:
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def terminate_pid(pid: int) -> None:
    try:
        os.kill(pid, signal.SIGTERM)
    except OSError:
        return
    deadline = time.time() + 2.5
    while time.time() < deadline:
        if not is_pid_alive(pid):
            return
        time.sleep(0.1)
    try:
        os.kill(pid, signal.SIGKILL)
    except OSError:
        return


def wait_for_port_release(port: int, timeout_seconds: float = 4.0) -> bool:
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        check_shutdown_requested()
        if not listener_pids(port):
            return True
        time.sleep(0.1)
    return not listener_pids(port)


def sanitize_node_options(raw: str) -> str:
    raw = raw.strip()
    if not raw:
        return ""
    try:
        parts = shlex.split(raw)
    except ValueError:
        parts = raw.split()

    filtered: list[str] = []
    index = 0
    while index < len(parts):
        token = parts[index]
        lower = token.lower()
        if lower.startswith("--inspect") or lower.startswith("--debug") or lower.startswith("--inspect-publish-uid"):
            index += 1
            continue
        if lower in {"--require", "-r"} and index + 1 < len(parts):
            required = parts[index + 1]
            required_lower = required.lower()
            if "js-debug" in required_lower or "bootloader.js" in required_lower:
                index += 2
                continue
        if lower.startswith("--require=") or lower.startswith("-r="):
            required_lower = lower.split("=", 1)[1]
            if "js-debug" in required_lower or "bootloader.js" in required_lower:
                index += 1
                continue
        filtered.append(token)
        index += 1
    return shlex.join(filtered)


def child_process_env() -> dict[str, str]:
    env = os.environ.copy()
    for key in DEBUG_ENV_KEYS:
        env.pop(key, None)
    node_options = sanitize_node_options(env.get("NODE_OPTIONS", ""))
    if node_options:
        env["NODE_OPTIONS"] = node_options
    else:
        env.pop("NODE_OPTIONS", None)
    return env


def looks_like_our_process(command: str, role: str) -> bool:
    lower = command.lower()
    root = str(SCRIPT_DIR).lower()
    if role == "backend":
        if root in lower and ("cmd/dashboard" in lower or "dashboard.py" in lower):
            return True
        if "go -c go run ./cmd/dashboard" in lower or "python dashboard.py" in lower:
            return True
        if "/go-build/" in lower and "/dashboard " in lower and "--state-file" in lower and "--report-file" in lower:
            return True
        return False
    if role == "frontend":
        if root in lower and ("vite" in lower or ("pnpm" in lower and " web " in f" {lower} " and " dev" in lower)):
            return True
        if "pnpm --dir web dev" in lower or "/vite/bin/vite.js" in lower:
            return True
        return False
    if role == "observer":
        if root in lower and "career_monitor.greenhouse_observer_api" in lower:
            return True
        if "uv run --extra playwright python -m career_monitor.greenhouse_observer_api" in lower:
            return True
        if "python -m career_monitor.greenhouse_observer_api" in lower:
            return True
        return False
    return False


def inspect_port_owners(port: int, role: str) -> tuple[list[tuple[int, str]], list[tuple[int, str]]]:
    pids = listener_pids(port)
    owned: list[tuple[int, str]] = []
    foreign: list[tuple[int, str]] = []
    for pid in pids:
        cmd = command_for_pid(pid)
        if looks_like_our_process(cmd, role):
            owned.append((pid, cmd))
        else:
            foreign.append((pid, cmd))
    return owned, foreign


def ensure_port_ready_for_app(port: int, role: str, health_url: str, force_restart: bool = False) -> bool:
    healthy = url_ready(health_url)
    owned, foreign = inspect_port_owners(port, role)

    if force_restart:
        if foreign:
            print(f"Port {port} is busy with another app. Refusing to kill it.")
            for pid, cmd in foreign[:2]:
                print(f"  pid={pid} cmd={cmd}")
            return False
        if not owned and not healthy:
            return True
    else:
        if healthy:
            return True
        if not owned and not foreign:
            return True
        if foreign and not owned:
            print(f"Port {port} is busy with another app. Refusing to kill it.")
            for pid, cmd in foreign[:2]:
                print(f"  pid={pid} cmd={cmd}")
            return False

    if owned:
        reason = "Restarting" if force_restart else "Stopping stale"
        print(f"{reason} {role} listeners on :{port} -> {' '.join(str(pid) for pid, _ in owned)}")
        for pid, _ in owned:
            terminate_pid(pid)
        if not wait_for_port_release(port):
            print(f"Port {port} did not free after stopping {role} listeners.")
            return False

    remaining = listener_pids(port)
    if remaining or (force_restart and url_ready(health_url)):
        print(f"Port {port} is still busy after {role} cleanup.")
        for pid in remaining[:2]:
            print(f"  pid={pid} cmd={command_for_pid(pid)}")
        return False
    return True


def terminate_process(process: subprocess.Popen[bytes] | None, name: str) -> None:
    if process is None:
        return
    if os.name == "nt":
        try:
            process.terminate()
        except OSError:
            return
    else:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except OSError:
            return
    try:
        process.wait(timeout=4)
    except subprocess.TimeoutExpired:
        if os.name == "nt":
            process.kill()
        else:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except OSError:
                return
        try:
            process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            return
    print(f"{name} stopped.")


def parse_args() -> tuple[bool, bool, list[str]]:
    api_only = False
    fresh = False
    passthrough: list[str] = []
    for arg in sys.argv[1:]:
        if arg == "--api-only":
            api_only = True
            continue
        if arg == "--fresh":
            fresh = True
            continue
        passthrough.append(arg)
    return api_only, fresh, passthrough


def rewrite_backend_paths(args: list[str]) -> list[str]:
    rewritten: list[str] = []
    i = 0
    while i < len(args):
        current = args[i]

        if current in PATH_FLAGS and i + 1 < len(args):
            raw_path = args[i + 1]
            absolute = Path(raw_path).expanduser()
            if not absolute.is_absolute():
                absolute = (Path.cwd() / absolute).resolve()
            rewritten.append(current)
            rewritten.append(os.path.relpath(str(absolute), str(GO_DIR)))
            i += 2
            continue

        rewritten_flag = current
        for flag in PATH_FLAGS:
            prefix = flag + "="
            if current.startswith(prefix):
                raw_path = current[len(prefix) :]
                absolute = Path(raw_path).expanduser()
                if not absolute.is_absolute():
                    absolute = (Path.cwd() / absolute).resolve()
                rewritten_flag = prefix + os.path.relpath(str(absolute), str(GO_DIR))
                break
        rewritten.append(rewritten_flag)
        i += 1

    return rewritten


def spawn_backend(args: list[str]) -> subprocess.Popen[bytes]:
    cmd = ["go", "-C", "go", "run", "./cmd/dashboard", *rewrite_backend_paths(args)]
    print(f"Starting backend: {' '.join(cmd)}")
    return subprocess.Popen(cmd, start_new_session=(os.name != "nt"), env=child_process_env())  # noqa: S603,S607


def spawn_frontend() -> subprocess.Popen[bytes]:
    cmd = ["pnpm", "--dir", "web", "dev", "--host", FRONTEND_HOST, "--port", str(FRONTEND_PORT), "--strictPort"]
    print(f"Starting frontend: {' '.join(cmd)}")
    return subprocess.Popen(cmd, start_new_session=(os.name != "nt"), env=child_process_env())  # noqa: S603,S607


def spawn_observer() -> subprocess.Popen[bytes]:
    cmd = [
        "uv",
        "run",
        "--extra",
        "playwright",
        "python",
        "-m",
        "career_monitor.greenhouse_observer_api",
        "--host",
        DEFAULT_OBSERVER_HOST,
        "--port",
        str(DEFAULT_OBSERVER_PORT),
        "--jobs-db-file",
        ".state/jobs.db",
    ]
    profile_path = (SCRIPT_DIR / DEFAULT_OBSERVER_PROFILE_FILE).resolve()
    if profile_path.exists():
        cmd.extend(["--profile-file", str(profile_path)])
    answers_path = (SCRIPT_DIR / DEFAULT_OBSERVER_ANSWERS_FILE).resolve()
    if answers_path.exists():
        cmd.extend(["--answers-file", str(answers_path)])
    env = child_process_env()
    env.setdefault("ORCHESTRATOR_SLM_PROVIDER", "ollama")
    env.setdefault("GREENHOUSE_SLM_ENABLED", "1")
    env.setdefault("GREENHOUSE_SLM_MODEL", "qwen2.5:3b")
    env.setdefault("GREENHOUSE_SLM_TIMEOUT_SECONDS", "20")
    print(f"Starting observer: {' '.join(cmd)}")
    return subprocess.Popen(cmd, start_new_session=(os.name != "nt"), env=env)  # noqa: S603,S607


def main() -> int:
    install_signal_handlers()
    api_only, fresh, backend_args = parse_args()
    backend_host, backend_port = parse_backend_host_port(backend_args)
    backend_url = backend_health_url(backend_host, backend_port)
    frontend_url = frontend_monitor_url()
    observer_url = observer_health_url(DEFAULT_OBSERVER_HOST, DEFAULT_OBSERVER_PORT)
    backend_process: subprocess.Popen[bytes] | None = None
    frontend_process: subprocess.Popen[bytes] | None = None
    observer_process: subprocess.Popen[bytes] | None = None
    monitor_url = os.getenv("FRONTEND_BASE_URL", f"http://{FRONTEND_HOST}:{FRONTEND_PORT}").rstrip("/") + "/monitor"
    backend_ready = False
    observer_ready = False
    frontend_ready = api_only

    try:
        check_shutdown_requested()
        if url_ready(backend_url) and not fresh:
            print(f"Backend already running on {backend_host}:{backend_port}.")
            backend_ready = True
        else:
            if not ensure_port_ready_for_app(backend_port, "backend", backend_url, force_restart=fresh):
                return 1
            backend_process = spawn_backend(backend_args)
            if not wait_for_url(backend_url, timeout_seconds=45):
                code = backend_process.poll()
                if code is None:
                    terminate_process(backend_process, "Backend")
                    return 1
                return int(code)
            backend_ready = True
            print(f"Backend ready: http://{backend_host}:{backend_port}/")

        check_shutdown_requested()
        if url_ready(observer_url) and not fresh:
            print(f"Observer already running on {DEFAULT_OBSERVER_HOST}:{DEFAULT_OBSERVER_PORT}.")
            observer_ready = True
        else:
            if not ensure_port_ready_for_app(DEFAULT_OBSERVER_PORT, "observer", observer_url, force_restart=fresh):
                terminate_process(backend_process, "Backend")
                return 1
            observer_process = spawn_observer()
            if not wait_for_url(observer_url, timeout_seconds=45):
                code = observer_process.poll()
                if code is None:
                    terminate_process(observer_process, "Observer")
                    terminate_process(backend_process, "Backend")
                    return 1
                terminate_process(backend_process, "Backend")
                return int(code)
            observer_ready = True
            print(f"Observer ready: http://{DEFAULT_OBSERVER_HOST}:{DEFAULT_OBSERVER_PORT}/")

        if api_only:
            print("API-only mode. Skipping frontend startup.")
        else:
            check_shutdown_requested()
            if url_ready(frontend_url) and not fresh:
                print(f"Frontend already running on {FRONTEND_HOST}:{FRONTEND_PORT}.")
                frontend_ready = True
            else:
                if not ensure_port_ready_for_app(FRONTEND_PORT, "frontend", frontend_url, force_restart=fresh):
                    terminate_process(observer_process, "Observer")
                    terminate_process(backend_process, "Backend")
                    return 1
                frontend_process = spawn_frontend()
                if not wait_for_url(frontend_url, timeout_seconds=45):
                    code = frontend_process.poll()
                    if code is not None:
                        terminate_process(observer_process, "Observer")
                        terminate_process(backend_process, "Backend")
                        return int(code)
                    print(f"Frontend process is still starting. Wait for Vite to print ready, then open {monitor_url}.")
                else:
                    frontend_ready = True
                    print(f"Frontend ready: {monitor_url}")

        if api_only:
            print(f"Managed services ready. Backend UI/API: http://{backend_host}:{backend_port}/")
            if observer_ready:
                print(f"Observer API: http://{DEFAULT_OBSERVER_HOST}:{DEFAULT_OBSERVER_PORT}/")
            print(f"Health check: {backend_url}")
            print("Press Ctrl-C to stop.")
        else:
            if backend_ready and observer_ready and frontend_ready:
                print(f"Managed services ready. Monitor UI: {monitor_url}")
                print(f"Backend API: http://{backend_host}:{backend_port}/")
                print(f"Observer API: http://{DEFAULT_OBSERVER_HOST}:{DEFAULT_OBSERVER_PORT}/")
                print("Press Ctrl-C to stop.")
            elif backend_ready:
                print(f"Backend is ready. Frontend is still starting: {monitor_url}")

        # If we didn't spawn anything, just return success.
        if backend_process is None and frontend_process is None and observer_process is None:
            return 0

        while True:
            check_shutdown_requested()
            if backend_process is not None:
                backend_code = backend_process.poll()
                if backend_code is not None:
                    terminate_process(observer_process, "Observer")
                    terminate_process(frontend_process, "Frontend")
                    return int(backend_code)
            if observer_process is not None:
                observer_code = observer_process.poll()
                if observer_code is not None:
                    terminate_process(frontend_process, "Frontend")
                    terminate_process(backend_process, "Backend")
                    return int(observer_code)
            if frontend_process is not None:
                frontend_code = frontend_process.poll()
                if frontend_code is not None:
                    terminate_process(observer_process, "Observer")
                    terminate_process(backend_process, "Backend")
                    return int(frontend_code)
            time.sleep(0.5)
    except (KeyboardInterrupt, ShutdownRequested):
        terminate_process(frontend_process, "Frontend")
        terminate_process(observer_process, "Observer")
        terminate_process(backend_process, "Backend")
        return 130


if __name__ == "__main__":
    raise SystemExit(main())
