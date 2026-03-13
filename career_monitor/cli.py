from __future__ import annotations

"""Compatibility wrapper for the legacy Python monitor runtime.

The Go runtime is the default production path. The legacy Python monitor now
lives under ``career_monitor.legacy`` so the repo structure has a single
obvious source of truth.
"""

from .legacy.cli import main, parse_args, run

__all__ = ["main", "parse_args", "run"]


if __name__ == "__main__":
    raise SystemExit(main())
