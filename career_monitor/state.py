from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from .utils import utc_now


def load_state(state_path: Path) -> dict[str, Any]:
    if not state_path.exists():
        return {"seen": {}, "company_status": {}, "blocked_events": {}, "last_run": None}
    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        state = {}
    state.setdefault("seen", {})
    state.setdefault("company_status", {})
    state.setdefault("blocked_events", {})
    state.setdefault("last_run", None)
    return state


def save_state(state_path: Path, state: dict[str, Any]) -> None:
    state_path.parent.mkdir(parents=True, exist_ok=True)
    state_path.write_text(json.dumps(state, indent=2, sort_keys=True), encoding="utf-8")


def update_last_run(state: dict[str, Any]) -> None:
    state["last_run"] = utc_now()

