#!/bin/bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

python3 - "$root/.dockerignore" <<'PY'
import sys
from pathlib import Path

patterns = Path(sys.argv[1]).read_text().splitlines()
if ".worktrees/" not in patterns:
    raise SystemExit(".dockerignore must exclude .worktrees/ from Docker build contexts")
PY
