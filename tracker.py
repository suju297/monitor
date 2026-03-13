#!/usr/bin/env python3
from __future__ import annotations

import subprocess
import sys


def main() -> int:
    cmd = ["go", "-C", "go", "run", "./cmd/monitor", *sys.argv[1:]]
    process = subprocess.run(cmd, check=False)  # noqa: S603,S607
    return int(process.returncode)


if __name__ == "__main__":
    raise SystemExit(main())
