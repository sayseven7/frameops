#!/bin/bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

expect_rejection() {
  local mutation=$1
  local case_dir="$tmpdir/$mutation"
  cp -R "$root/.github/workflows" "$case_dir"
  MUTATION="$mutation" CASE_DIR="$case_dir" python3 - <<'PY'
import os
from pathlib import Path

replacements = {
    "unpinned-action": ("3d3c42e5aac5ba805825da76410c181273ba90b1", "v7.0.1"),
    "write-permission": ("contents: read", "contents: write"),
    "job-write-all-permission": ("    timeout-minutes: 10", "    permissions: write-all\n    timeout-minutes: 10"),
    "flow-style-permission": ("permissions:\n  contents: read\n\nconcurrency:", "permissions: {contents: write}\n\nconcurrency:"),
    "quoted-write-permission": ("contents: read", 'issues: "write"'),
    "tagged-write-permission": ("contents: read", "issues: !!str write"),
    "anchored-write-permission": ("contents: read", "issues: &scope write"),
    "aliased-write-permission": ("contents: read\n  security-events: write", "security-events: &scope write\n  contents: *scope"),
    "escaped-write-permission": ("contents: read", 'contents: "\\x77rite"'),
    "unpinned-reusable-workflow": ("name: Dependency review", "name: Dependency review"),
    "quoted-uses": ("- uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "- 'uses': evil-org/evil-action@v1"),
    "flow-style-uses": ("      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n        with:\n          persist-credentials: false", "      - {uses: evil-org/evil-action@v1, with: {persist-credentials: false}}"),
    "anchored-uses": ("- uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", "- uses: &action evil-org/evil-action@v1"),
    "folded-uses": ("      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1", "      - uses: >-\n          evil-org/evil-action@v1"),
    "pull-request-target": ("pull_request:", "pull_request_target:"),
    "quoted-pull-request-target": ("pull_request:", "'pull_request_target':"),
    "checkout-credentials": ("persist-credentials: false", "persist-credentials: true"),
    "missing-timeout": ("timeout-minutes", "limit-minutes"),
    "missing-concurrency": ("concurrency:", "coordination:"),
    "missing-sarif-guard": ("if: always() && hashFiles('trivy-results.sarif') != ''", "if: always()"),
    "detached-sarif-guard": (
        "        if: always() && hashFiles('trivy-results.sarif') != ''\n        with:\n          sarif_file: trivy-results.sarif",
        "        if: always()\n        with:\n          sarif_file: trivy-results.sarif\n      - if: always() && hashFiles('trivy-results.sarif') != ''\n        run: 'true'",
    ),
    "renamed-sarif-workflow": ("if: always() && hashFiles('trivy-results.sarif') != ''", "if: always()"),
    "golangci-build-toolchain": ("GOTOOLCHAIN=go1.26.5 ", ""),
    "expression-sarif-guard": ("if: always() && hashFiles('trivy-results.sarif') != ''", "if: ${{ always() }}"),
}
old, new = replacements[os.environ["MUTATION"]]
replaced = False
for path in Path(os.environ["CASE_DIR"]).glob("*.yml"):
    if os.environ["MUTATION"] == "flow-style-uses" and path.name != "ci.yml":
        continue
    source = path.read_text()
    if old in source:
        path.write_text(source.replace(old, new))
        replaced = True
if os.environ["MUTATION"] == "unpinned-reusable-workflow":
    path = Path(os.environ["CASE_DIR"]) / "dependency-review.yml"
    path.write_text(path.read_text() + "\n  reusable:\n    uses: evil-org/evil-workflow@v1\n")
    replaced = True
if os.environ["MUTATION"] == "renamed-sarif-workflow":
    path = Path(os.environ["CASE_DIR"]) / "trivy.yml"
    path.rename(Path(os.environ["CASE_DIR"]) / "renamed.yml")
if not replaced:
    raise SystemExit(f"mutation did not match: {os.environ['MUTATION']}")
PY
  yaml_files=()
  for path in "$case_dir"/*.yml "$case_dir"/*.yaml; do
    [[ -e "$path" ]] && yaml_files+=("/work/${path##*/}")
  done
  actionlint_output=$(docker run --rm --network none -v "$case_dir:/work:ro" -w /work \
    rhysd/actionlint@sha256:ef8299f97635c4c30e2298f48f30763ab782a4ad2c95b744649439a039421e36 \
    -no-color "${yaml_files[@]}" 2>&1) || {
    if [[ "$actionlint_output" == *"could not parse as YAML"* ]]; then
      printf 'FAIL: invalid YAML fixture %s\n%s\n' "$mutation" "$actionlint_output" >&2
      exit 1
    fi
  }
  if WORKFLOWS_DIR="$case_dir" bash "$root/scripts/check-github-actions.sh" >/dev/null 2>&1; then
    printf 'FAIL: accepted %s\n' "$mutation" >&2
    exit 1
  fi
  printf 'PASS: rejects %s\n' "$mutation"
}

expect_gitlink_rejection() {
  local repo="$tmpdir/gitlink-repo"
  git init -q "$repo"
  printf '.worktrees/\n' >"$repo/.gitignore"
  git -C "$repo" update-index --add --cacheinfo \
    160000,2c07696d31945f867b4dc34a6035341449c67124,.worktrees/bad
  if (cd "$repo" && WORKFLOWS_DIR="$root/.github/workflows" bash "$root/scripts/check-github-actions.sh" >/dev/null 2>&1); then
    printf 'FAIL: accepted tracked .worktrees gitlink\n' >&2
    exit 1
  fi
  printf 'PASS: rejects tracked .worktrees gitlink\n'
}

bash "$root/scripts/check-github-actions.sh"
for mutation in unpinned-action write-permission job-write-all-permission flow-style-permission quoted-write-permission tagged-write-permission anchored-write-permission aliased-write-permission escaped-write-permission unpinned-reusable-workflow quoted-uses flow-style-uses anchored-uses folded-uses pull-request-target quoted-pull-request-target checkout-credentials missing-timeout missing-concurrency missing-sarif-guard detached-sarif-guard renamed-sarif-workflow golangci-build-toolchain expression-sarif-guard; do
  expect_rejection "$mutation"
done
expect_gitlink_rejection
