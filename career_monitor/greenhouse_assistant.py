from __future__ import annotations

import sys

from .greenhouse import _implementation as _implementation

# Alias the historical module path to the live implementation module so patching
# career_monitor.greenhouse_assistant.* still affects function globals.
sys.modules[__name__] = _implementation

