#!/bin/bash
set -euo pipefail

workflows_dir=${WORKFLOWS_DIR:-.github/workflows}

if [[ ! -d "$workflows_dir" ]]; then
  printf 'missing workflow directory: %s\n' "$workflows_dir" >&2
  exit 1
fi

shopt -s nullglob
workflows=("$workflows_dir"/*.yml "$workflows_dir"/*.yaml)
if ((${#workflows[@]} == 0)); then
  printf 'no workflow files in %s\n' "$workflows_dir" >&2
  exit 1
fi

WORKFLOWS="$(printf '%s\n' "${workflows[@]}")" python3 - <<'PY'
import os
import re
import sys
from pathlib import Path

errors = []
for name in filter(None, os.environ["WORKFLOWS"].splitlines()):
    path = Path(name)
    text = path.read_text()
    prefix = f"{path}:"
    if "pull_request_target" in text:
        errors.append(f"{prefix} pull_request_target is prohibited")
    lines = text.splitlines()
    permission_blocks = []
    for index, line in enumerate(lines):
        if re.match(r"[ \t]*permissions(?:[ \t]*:|[ \t])", line):
            if re.fullmatch(r"[ \t]*permissions:[ \t]*", line):
                permission_blocks.append(index)
            else:
                errors.append(f"{prefix} permissions must use a block mapping")
    if not permission_blocks:
        errors.append(f"{prefix} missing workflow permissions")
    for start in permission_blocks:
        found_permission = False
        for line in lines[start + 1:]:
            if not line.strip():
                continue
            if not re.match(r"[ \t]", line):
                break
            permission = re.fullmatch(r"[ \t]+([A-Za-z0-9-]+): (read|none|write)", line)
            if not permission:
                errors.append(f"{prefix} invalid permission entry")
                continue
            found_permission = True
            scope, value = permission.groups()
            if value == "write" and scope not in {"security-events", "id-token"}:
                errors.append(f"{prefix} undue write permission: {scope}")
        if not found_permission:
            errors.append(f"{prefix} empty permissions block")
    if not re.search(r"(?m)^concurrency\s*:", text):
        errors.append(f"{prefix} missing concurrency")
    if not re.search(r"(?m)^\s+timeout-minutes\s*:\s*\d+\s*$", text):
        errors.append(f"{prefix} missing job timeout-minutes")
    for line in lines:
        expression_free = re.sub(r"\$\{\{[^\n}]*\}\}", "", line)
        if re.search(r"^[ \t]*(?:-\s*)?(?:[\"']|[&*!?{])", line):
            errors.append(f"{prefix} unsupported YAML key syntax")
            break
        uses = re.match(r"^[ \t]*(?:-\s*)?uses\s*:(.*)$", line)
        if uses:
            # ponytail: plain pinned refs only; use a pinned parser if dynamic refs are needed.
            value = re.sub(r"\s+#.*$", "", uses.group(1)).strip()
            if not re.fullmatch(r"[^@\s]+@[0-9a-f]{40}", value):
                errors.append(f"{prefix} uses is not pinned to a full SHA")
    for checkout in re.finditer(r"(?m)^\s*-\s*uses:\s*actions/checkout@[0-9a-f]{40}[^\n]*", text):
        step = re.split(r"(?m)^\s*-\s", text[checkout.end():], maxsplit=1)[0]
        if not re.search(r"(?m)^\s+persist-credentials:\s*false\s*$", step):
            errors.append(f"{prefix} checkout must set persist-credentials: false")

if errors:
    print("GitHub Actions contract check failed:", file=sys.stderr)
    print(*[f"- {error}" for error in errors], sep="\n", file=sys.stderr)
    sys.exit(1)
PY
