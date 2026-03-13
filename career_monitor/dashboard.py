from __future__ import annotations

"""Compatibility wrapper for the legacy Python dashboard runtime.

The Go dashboard is the default production path. The legacy Python dashboard
now lives under ``career_monitor.legacy`` so it is clearly separated from the
current runtime.
"""

from .legacy.dashboard import main, parse_args, run_dashboard

__all__ = ["main", "parse_args", "run_dashboard"]


if __name__ == "__main__":
    raise SystemExit(main())
