#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
# shellcheck disable=SC1091 # Runtime-computed repository root.
. "$root/scripts/lib/backup-path.sh"

store_path="${MISSIS_STORE:-.missis-store/missis.db}"
backup_dir="$(resolve_missis_backup_dir "$store_path")"
manifest="$(MISSIS_STORE="$store_path" go run ./tools/missis-tools manifest)"
store_id="$(printf '%s' "$manifest" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
head_hash="$(printf '%s' "$manifest" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"
mkdir -p "$backup_dir"
backup_name="${store_id//:/_}-${head_hash}.db"
dst="$backup_dir/$backup_name"

if [ -f "$dst" ]; then
  echo "backup already exists: $dst (identical head is safe to skip)"
  exit 0
fi

MISSIS_STORE="$store_path" go run ./tools/missis-tools backup "$dst"
echo "$dst"

# Optional R2/S3 upload:
# rclone copy "$dst" remote:missis-backups/
# aws s3 cp "$dst" s3://your-bucket/missis-backups/
# Safety: filename is keyed by store-id and head-hash, so a different head
# cannot overwrite an existing backup without an explicit force step.
