#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

store_path="${MISSIS_STORE:-.missis-store/missis.db}"
manifest_path="${MISSIS_MANIFEST_PATH:-.missis.d/manifest.json}"
manifest="$(MISSIS_STORE="$store_path" MISSIS_MANIFEST_PATH="$manifest_path" bash "$root/scripts/write-store-manifest.sh" >/dev/null; cat "$manifest_path")"
store_id="$(printf '%s' "$manifest" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
head_hash="$(printf '%s' "$manifest" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"
mkdir -p backups
backup_name="${store_id//:/_}-${head_hash}.db"
dst="backups/$backup_name"

if [ -f "$dst" ]; then
  echo "backup already exists: $dst (identical head is safe to skip)"
  exit 0
fi

MISSIS_STORE="$store_path" go run ./tools/store-backup "$dst"
echo "$dst"

# Optional R2/S3 upload:
# rclone copy "$dst" remote:missis-backups/
# aws s3 cp "$dst" s3://your-bucket/missis-backups/
# Safety: filename is keyed by store-id and head-hash, so a different head
# cannot overwrite an existing backup without an explicit force step.
