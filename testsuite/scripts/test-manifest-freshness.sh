#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
real_go="$(command -v go)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

store_path="$tmp/store.db"
backup_dir="$tmp/backups"
stale_manifest="$tmp/stale-manifest.json"

(
  cd "$repo_root"
  "$real_go" run ./cmd/missis new --store "$store_path" "manifest before mutation" >/dev/null
  MISSIS_STORE="$store_path" "$real_go" run ./tools/missis-tools manifest > "$stale_manifest"
  "$real_go" run ./cmd/missis new --store "$store_path" "manifest after mutation" >/dev/null
)

current_manifest="$(
  cd "$repo_root"
  MISSIS_STORE="$store_path" "$real_go" run ./tools/missis-tools manifest
)"
stale_head="$(sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p' "$stale_manifest")"
current_head="$(printf '%s\n' "$current_manifest" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"

if [ -z "$stale_head" ] || [ -z "$current_head" ] || [ "$stale_head" = "$current_head" ]; then
  echo "fixture did not create a stale manifest" >&2
  exit 1
fi

stale_before="$(<"$stale_manifest")"
backup_output="$(
  cd "$repo_root"
  MISSIS_STORE="$store_path" \
    MISSIS_BACKUP_DIR="$backup_dir" \
    MISSIS_MANIFEST_PATH="$stale_manifest" \
    bash scripts/backup-store.sh
)"
store_id="$(printf '%s\n' "$current_manifest" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
expected_backup="$backup_dir/${store_id//:/_}-${current_head}.db"

if [ ! -f "$expected_backup" ]; then
  echo "backup did not use the live manifest: $backup_output" >&2
  exit 1
fi
if [ "$(<"$stale_manifest")" != "$stale_before" ]; then
  echo "backup unexpectedly rewrote the stale manifest path" >&2
  exit 1
fi

(
  cd "$repo_root"
  MISSIS_STORE="$store_path" \
    MISSIS_BACKUP_DIR="$backup_dir" \
    MISSIS_MANIFEST_PATH="$stale_manifest" \
    bash scripts/verify-restore.sh "$expected_backup" >/dev/null
  MISSIS_STORE="$store_path" \
    MISSIS_BACKUP_DIR="$backup_dir" \
    MISSIS_MANIFEST_PATH="$stale_manifest" \
    bash scripts/verify-restore.sh >/dev/null
)

external_project="$tmp/external-project"
external_store="$external_project/.missis-store/missis.db"
mkdir -p "$(dirname "$external_store")"
(
  cd "$repo_root"
  "$real_go" run ./cmd/missis new --store "$external_store" "external default backup" >/dev/null
)
external_manifest="$(
  cd "$repo_root"
  MISSIS_STORE="$external_store" "$real_go" run ./tools/missis-tools manifest
)"
external_store_id="$(printf '%s\n' "$external_manifest" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
external_head="$(printf '%s\n' "$external_manifest" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"
external_expected="$external_project/.missis-backups/${external_store_id//:/_}-${external_head}.db"
external_output="$(
  cd "$repo_root"
  env -u MISSIS_BACKUP_DIR MISSIS_STORE="$external_store" bash scripts/backup-store.sh
)"

if [ "$external_output" != "$external_expected" ] || [ ! -f "$external_expected" ]; then
  echo "external store used the wrong default backup directory: $external_output" >&2
  exit 1
fi

(
  cd "$repo_root"
  env -u MISSIS_BACKUP_DIR MISSIS_STORE="$external_store" \
    bash scripts/verify-restore.sh >/dev/null
)

echo "external default backup path tests passed"
echo "manifest freshness tests passed"
