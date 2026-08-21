#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [ "$#" -gt 1 ]; then
  echo "usage: verify-restore.sh [backup.db]" >&2
  exit 2
fi

store_path="${MISSIS_STORE:-.missis-store/missis.db}"
backup_dir="${MISSIS_BACKUP_DIR:-backups}"
manifest="$(MISSIS_STORE="$store_path" go run ./tools/missis-tools manifest)"
store_id="$(printf '%s' "$manifest" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
head_hash="$(printf '%s' "$manifest" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"

if [ "$#" -eq 1 ]; then
  backup="$1"
else
  backup="$backup_dir/${store_id//:/_}-${head_hash}.db"
fi

if [ ! -f "$backup" ]; then
  echo "backup not found: $backup" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
cp "$backup" "$tmpdir/restore.db"

restored="$(MISSIS_STORE="$tmpdir/restore.db" go run ./tools/missis-tools manifest)"
restored_id="$(printf '%s' "$restored" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
restored_head="$(printf '%s' "$restored" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"
restored_schema="$(printf '%s' "$restored" | sed -n 's/.*"schema_version": "\([^"]*\)".*/\1/p')"
restored_events="$(printf '%s' "$restored" | sed -n 's/.*"event_count": \([0-9]*\).*/\1/p')"
expected_schema="$(printf '%s' "$manifest" | sed -n 's/.*"schema_version": "\([^"]*\)".*/\1/p')"
expected_events="$(printf '%s' "$manifest" | sed -n 's/.*"event_count": \([0-9]*\).*/\1/p')"

if [ "$restored_id" != "$store_id" ] || [ "$restored_head" != "$head_hash" ] || \
   [ "$restored_schema" != "$expected_schema" ] || [ "$restored_events" != "$expected_events" ]; then
  echo "restore verification failed" >&2
  exit 1
fi

echo "restore verified"
