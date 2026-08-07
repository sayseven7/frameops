#!/bin/bash
set -euo pipefail

expected=9f7cc1056521be0d213a3e2f418e29e6faf72d6967eb1920752d7627a03211de
actual=$(sha256sum migrations/00013_report_pdfs.sql | cut -d ' ' -f1)
[[ $actual == "$expected" ]] || {
  printf 'migration 00013 is immutable: got %s, want %s\n' "$actual" "$expected" >&2
  exit 1
}

migration=migrations/00017_report_pdf_reservation_recovery.sql
for required in \
  'DROP INDEX report_pdfs_one_stored_per_revision_idx' \
  "state IN ('pending', 'stored', 'failed')" \
  "state IN ('pending', 'stored')" \
  'CREATE UNIQUE INDEX report_pdfs_one_effective_per_revision_key' \
  "OLD.state = 'pending' AND NEW.state = 'failed'" \
  "derived_at <= now() - interval '5 minutes'"; do
  grep -Fq "$required" "$migration" || {
    printf '%s must contain %q\n' "$migration" "$required" >&2
    exit 1
  }
done
