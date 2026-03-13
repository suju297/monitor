from __future__ import annotations

from pathlib import Path
from typing import Iterable


WORKSPACE_MARKERS = ("career_monitor", "go", "web", "README.md")


def workspace_root(start: Path | None = None) -> Path:
    current = (start or Path.cwd()).resolve()
    candidates = (current, *current.parents)
    for candidate in candidates:
        if all((candidate / marker).exists() for marker in WORKSPACE_MARKERS):
            return candidate
    return current


def state_path(*parts: str) -> Path:
    return workspace_root() / ".state" / Path(*parts)


def local_path(*parts: str) -> Path:
    return workspace_root() / ".local" / Path(*parts)


def prefer_local_file(raw_path: str | Path, local_name: str) -> Path:
    raw = Path(raw_path).expanduser()
    if raw.is_absolute():
        return raw

    current_abs = (Path.cwd() / raw).resolve(strict=False)
    preferred = local_path(local_name)

    if preferred.exists():
        return preferred
    if current_abs.exists():
        return current_abs
    if raw.name == local_name:
        return preferred
    return current_abs


def prefer_legacy_or_local(local_name: str, legacy_relative: str) -> Path:
    preferred = local_path(local_name)
    legacy = (workspace_root() / legacy_relative).resolve(strict=False)
    if preferred.exists():
        return preferred
    if legacy.exists():
        return legacy
    return preferred


def oauth_client_json_candidates() -> list[Path]:
    root = workspace_root()
    candidates: list[Path] = []
    seen: set[Path] = set()

    def add(path: Path) -> None:
        normalized = path.expanduser()
        if not normalized.is_absolute():
            normalized = (Path.cwd() / normalized).resolve(strict=False)
        if normalized in seen:
            return
        seen.add(normalized)
        candidates.append(normalized)

    for match in sorted(local_path().glob("client_secret_*.json")):
        add(match)
    for match in sorted(root.glob("client_secret_*.json")):
        add(match)
    return candidates


def first_existing(paths: Iterable[Path]) -> Path | None:
    for path in paths:
        if path.exists():
            return path
    return None
