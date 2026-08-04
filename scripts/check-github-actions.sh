#!/bin/bash
set -euo pipefail

workflows_dir=${WORKFLOWS_DIR:-.github/workflows}

if [[ ! -d "$workflows_dir" ]]; then
  printf 'missing workflow directory: %s\n' "$workflows_dir" >&2
  exit 1
fi

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  if git ls-files --stage .worktrees | grep -q '^160000 '; then
    printf 'tracked .worktrees gitlinks break GitHub checkout\n' >&2
    exit 1
  fi
  if ! git check-ignore -q .worktrees/contract-probe; then
    printf '.worktrees/ must be ignored\n' >&2
    exit 1
  fi
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
    if re.search(r"\bcorepack\s+enable\b", text):
        errors.append(f"{prefix} corepack bootstrap is prohibited; use pinned pnpm/action-setup")
    if re.search(r"(?m)^\s*- run:.*\bpnpm\b", text) and "pnpm/action-setup@" not in text:
        errors.append(f"{prefix} pnpm commands require pinned pnpm/action-setup")
    for run in re.finditer(r"(?m)^(?P<indent>\s*)- run:\s*(?P<command>[^\n]*)$", text):
        step = re.split(rf"(?m)^{re.escape(run.group('indent'))}-\s", text[run.end():], maxsplit=1)[0]
        if "pnpm" in run.group("command") + step and not re.fullmatch(r"pnpm [^;&|]+", run.group("command")):
            errors.append(f"{prefix} pnpm run steps must invoke pnpm directly")
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
    for install in re.finditer(r"(?m)^\s*- run: (.*go install github\.com/golangci/golangci-lint/[^\n]+)$", text):
        if not install.group(1).startswith("GOTOOLCHAIN=go1.26.5 "):
            errors.append(f"{prefix} golangci-lint must be built with Go 1.26.5")
    for init in re.finditer(r"(?m)^(?P<indent>\s*)- uses: github/codeql-action/init@[^\s#]+[^\n]*", text):
        step = re.split(rf"(?m)^{re.escape(init.group('indent'))}-\s", text[init.end():], maxsplit=1)[0]
        if not re.search(r"(?m)^\s+- language:\s*go\s*\n\s+build-mode:\s*autobuild\s*$", text):
            errors.append(f"{prefix} CodeQL Go matrix entry must use autobuild")
        if not re.search(r"(?m)^\s+build-mode:\s*\$\{\{\s*matrix\.build-mode\s*\}\}\s*$", step):
            errors.append(f"{prefix} CodeQL init must use the matrix build mode directly")
        if re.search(r"(?m)^\s+- language:\s*go\s*$", text) and re.search(r"(?m)^\s+build-mode:\s*none\s*$", step):
            errors.append(f"{prefix} CodeQL Go analysis cannot use build-mode none")
    for upload in re.finditer(
        r"(?m)^(?P<indent>\s*)- uses: github/codeql-action/upload-sarif@[^\s#]+[^\n]*",
        text,
    ):
        indent = upload.group("indent")
        step = re.split(rf"(?m)^{re.escape(indent)}-\s", text[upload.end():], maxsplit=1)[0]
        sarif = re.search(r"(?m)^\s+sarif_file:\s*([^\s#]+)\s*$", step)
        if re.search(r"(?m)^\s+if:.*always\(\)", step):
            expected = re.escape(indent + "  ") + rf"if: always\(\) && hashFiles\('{re.escape(sarif.group(1) if sarif else '')}'\) != ''"
            if sarif is None or not re.search(rf"(?m)^{expected}\s*$", step):
                errors.append(f"{prefix} always-run SARIF upload must require its output file")
    trivy_sarif = False
    trivy_gate = False
    if "aquasecurity/trivy-action@" in text and re.search(r"(?m)^    if:\s*", text):
        errors.append(f"{prefix} Trivy job must be unconditional")
    for trivy in re.finditer(r"(?m)^(?P<indent>\s*)- (?:name:[^\n]*\n(?P=indent)  )?uses: aquasecurity/trivy-action@[^\s#]+[^\n]*", text):
        step = re.split(rf"(?m)^{re.escape(trivy.group('indent'))}-\s", text[trivy.end():], maxsplit=1)[0]
        if re.search(r"(?m)^\s+format:\s*sarif\s*$", step):
            trivy_sarif = True
            if not re.search(r"(?m)^\s+exit-code:\s*['\"]?0['\"]?\s*$", step):
                errors.append(f"{prefix} Trivy SARIF reporting must not be the severity gate")
        if (
            re.search(r"(?m)^\s+format:\s*table\s*$", step)
            and re.search(r"(?m)^\s+severity:\s*HIGH,CRITICAL\s*$", step)
            and re.search(r"(?m)^\s+exit-code:\s*['\"]?1['\"]?\s*$", step)
            and not re.search(r"(?m)^\s+if:\s*", step)
        ):
            trivy_gate = True
    if trivy_sarif and not trivy_gate:
        errors.append(f"{prefix} Trivy SARIF reporting requires a separate HIGH/CRITICAL gate")

if errors:
    print("GitHub Actions contract check failed:", file=sys.stderr)
    print(*[f"- {error}" for error in errors], sep="\n", file=sys.stderr)
    sys.exit(1)
PY
