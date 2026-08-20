#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

manifest_path="${MISSIS_MANIFEST_PATH:-.missis.d/manifest.json}"
manifest="$(cat "$manifest_path")"
store_id="$(printf '%s' "$manifest" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
head_hash="$(printf '%s' "$manifest" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"
backup="backups/${store_id//:/_}-${head_hash}.db"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
cp "$backup" "$tmpdir/restore.db"

restored="$(MISSIS_STORE="$tmpdir/restore.db" go run ./tools/missis-tools manifest)"
restored_id="$(printf '%s' "$restored" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
restored_head="$(printf '%s' "$restored" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"

if [ "$restored_id" != "$store_id" ] || [ "$restored_head" != "$head_hash" ]; then
  echo "restore verification failed" >&2
  exit 1
fi

echo "restore verified"
