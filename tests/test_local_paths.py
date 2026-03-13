from __future__ import annotations

from pathlib import Path

from career_monitor.local_paths import prefer_legacy_or_local, prefer_local_file


def make_workspace(root: Path) -> None:
    (root / "career_monitor").mkdir()
    (root / "go").mkdir()
    (root / "web").mkdir()
    (root / "README.md").write_text("# test\n", encoding="utf-8")


def test_prefer_local_file_uses_local_variant_when_present(tmp_path: Path, monkeypatch) -> None:
    make_workspace(tmp_path)
    (tmp_path / ".local").mkdir()
    (tmp_path / ".local" / "companies.yaml").write_text("companies: []\n", encoding="utf-8")
    (tmp_path / "companies.yaml").write_text("companies: []\n", encoding="utf-8")
    monkeypatch.chdir(tmp_path)

    resolved = prefer_local_file("companies.yaml", "companies.yaml")

    assert resolved == tmp_path / ".local" / "companies.yaml"


def test_prefer_local_file_falls_back_to_legacy_file(tmp_path: Path, monkeypatch) -> None:
    make_workspace(tmp_path)
    (tmp_path / "companies.yaml").write_text("companies: []\n", encoding="utf-8")
    monkeypatch.chdir(tmp_path)

    resolved = prefer_local_file("companies.yaml", "companies.yaml")

    assert resolved == tmp_path / "companies.yaml"


def test_prefer_legacy_or_local_prefers_local_and_falls_back(tmp_path: Path, monkeypatch) -> None:
    make_workspace(tmp_path)
    (tmp_path / ".local").mkdir()
    (tmp_path / ".local" / "sample_state.json").write_text("{}", encoding="utf-8")
    monkeypatch.chdir(tmp_path)

    resolved_local = prefer_legacy_or_local(
        "sample_state.json",
        ".state/sample_state.json",
    )
    assert resolved_local == tmp_path / ".local" / "sample_state.json"

    (tmp_path / ".local" / "sample_state.json").unlink()
    (tmp_path / ".state").mkdir()
    (tmp_path / ".state" / "sample_state.json").write_text("{}", encoding="utf-8")

    resolved_legacy = prefer_legacy_or_local(
        "sample_state.json",
        ".state/sample_state.json",
    )
    assert resolved_legacy == tmp_path / ".state" / "sample_state.json"
