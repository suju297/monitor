from __future__ import annotations

import career_monitor.greenhouse._implementation as implementation
import career_monitor.greenhouse_assistant as legacy


def test_greenhouse_assistant_module_is_implementation_alias() -> None:
    assert legacy is implementation

