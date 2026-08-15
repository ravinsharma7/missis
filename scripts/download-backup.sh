#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [ -f ".env.local" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      \#*|'') ;;
      *=*) export "$line" ;;
    esac
  done < .env.local
fi

manifest="$(cat issues/missis-store/manifest.json)"
store_id="$(printf '%s' "$manifest" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
head_hash="$(printf '%s' "$manifest" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"

remote="${MISSIS_RCLONE_REMOTE:-}"
if [ -z "$remote" ]; then
  echo "MISSIS_RCLONE_REMOTE is not set" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
dest="$tmpdir/$head_hash.db"

rclone copy "$remote/$store_id/$head_hash.db" "$tmpdir"

restored="$(MISSIS_STORE="$dest" go run ./tools/store-manifest)"
restored_id="$(printf '%s' "$restored" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
restored_head="$(printf '%s' "$restored" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"

if [ "$restored_id" != "$store_id" ] || [ "$restored_head" != "$head_hash" ]; then
  echo "downloaded backup verification failed" >&2
  exit 1
fi

echo "downloaded backup verified"
