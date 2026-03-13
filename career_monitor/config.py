from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml

from .constants import DEFAULT_TIMEOUT_SECONDS, SUPPORTED_SOURCES
from .local_paths import prefer_local_file


def _normalized_fallback_sources(raw_value: Any, company_name: str) -> list[str]:
    if isinstance(raw_value, str):
        values = [item.strip().lower() for item in raw_value.split(",")]
    elif isinstance(raw_value, list):
        values = [str(item).strip().lower() for item in raw_value]
    else:
        raise ValueError(
            f"Company '{company_name}' has invalid fallback_sources. Use list or comma-separated string."
        )
    normalized = [value for value in values if value]
    for source in normalized:
        if source not in SUPPORTED_SOURCES:
            raise ValueError(f"Company '{company_name}' has unsupported fallback source '{source}'.")
    return normalized


def load_companies(config_path: Path) -> list[dict[str, Any]]:
    config_path = prefer_local_file(config_path, "companies.yaml")
    if not config_path.exists():
        raise FileNotFoundError(
            f"Config file not found: {config_path}. Copy companies.example.yaml to .local/companies.yaml or companies.yaml."
        )

    payload = yaml.safe_load(config_path.read_text(encoding="utf-8")) or {}
    companies = payload.get("companies")
    if not isinstance(companies, list) or not companies:
        raise ValueError("Config must contain a non-empty 'companies' list.")

    normalized: list[dict[str, Any]] = []
    for idx, entry in enumerate(companies, start=1):
        if not isinstance(entry, dict):
            raise ValueError(f"Company entry #{idx} must be an object.")
        if bool(entry.get("disabled", False)):
            continue

        name = str(entry.get("name", "")).strip()
        source = str(entry.get("source", "")).strip().lower()
        careers_url = str(entry.get("careers_url", "")).strip()
        if not name:
            raise ValueError(f"Company entry #{idx} is missing 'name'.")
        if source not in SUPPORTED_SOURCES:
            raise ValueError(
                f"Company '{name}' has unsupported source '{source}'. "
                f"Supported values: {', '.join(sorted(SUPPORTED_SOURCES))}."
            )
        if not careers_url:
            raise ValueError(f"Company '{name}' is missing 'careers_url'.")

        fallback_sources = _normalized_fallback_sources(entry.get("fallback_sources", []), name)
        timeout_seconds = max(5, int(entry.get("timeout_seconds", DEFAULT_TIMEOUT_SECONDS)))
        command_env_raw = entry.get("command_env", {})
        if command_env_raw is None:
            command_env_raw = {}
        if not isinstance(command_env_raw, dict):
            raise ValueError(f"Company '{name}' has invalid command_env. Use a key/value object.")
        command_env = {str(key): str(value) for key, value in command_env_raw.items()}

        normalized.append(
            {
                "name": name,
                "source": source,
                "careers_url": careers_url,
                "fallback_sources": fallback_sources,
                "timeout_seconds": timeout_seconds,
                "max_links": int(entry.get("max_links", 200)),
                "greenhouse_board": entry.get("greenhouse_board"),
                "lever_site": entry.get("lever_site"),
                "template": entry.get("template", {}),
                "command": entry.get("command"),
                "command_env": command_env,
                "my_greenhouse_command": entry.get("my_greenhouse_command"),
                "orchestration": entry.get("orchestration", {}),
            }
        )
    return normalized
