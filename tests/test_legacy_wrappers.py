from __future__ import annotations

from career_monitor import cli as cli_wrapper
from career_monitor import dashboard as dashboard_wrapper
from career_monitor.legacy import cli as legacy_cli
from career_monitor.legacy import dashboard as legacy_dashboard
from career_monitor.adapters import MY_GREENHOUSE_DEFAULT_COMMAND


def test_cli_wrapper_points_to_legacy_runtime() -> None:
    assert cli_wrapper.main is legacy_cli.main
    assert cli_wrapper.run is legacy_cli.run


def test_dashboard_wrapper_points_to_legacy_runtime() -> None:
    assert dashboard_wrapper.main is legacy_dashboard.main
    assert dashboard_wrapper.run_dashboard is legacy_dashboard.run_dashboard


def test_my_greenhouse_default_command_uses_module_execution() -> None:
    assert MY_GREENHOUSE_DEFAULT_COMMAND == [
        "uv",
        "run",
        "python",
        "-m",
        "scripts.fetch_my_greenhouse_jobs",
    ]
