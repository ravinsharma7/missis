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
backup="backups/${store_id//:/_}-${head_hash}.db"

if [ ! -f "$backup" ]; then
  echo "backup not found: $backup" >&2
  exit 1
fi

if [ -n "${MISSIS_REMOTE_DIR:-}" ]; then
  remote_dir="$MISSIS_REMOTE_DIR"
  dest="$remote_dir/$store_id/$head_hash.db"
  mkdir -p "$(dirname "$dest")"
  if [ -f "$dest" ] && [ "${MISSIS_BACKUP_FORCE:-0}" != "1" ]; then
    echo "remote backup already exists: $dest"
    exit 0
  fi
  cp "$backup" "$dest"
  echo "uploaded $dest"
  exit 0
fi

if command -v rclone >/dev/null 2>&1; then
  remote_dest="${MISSIS_RCLONE_REMOTE:-}"
  if [ -z "$remote_dest" ]; then
    echo "MISSIS_RCLONE_REMOTE is not set" >&2
    exit 1
  fi
  tmpconfig="$(mktemp)"
  trap 'rm -f "$tmpconfig"' EXIT
  cat > "$tmpconfig" <<EOF
[missis]
type = s3
provider = Cloudflare
access_key_id = $RCLONE_CONFIG_MISSIS_ACCESS_KEY_ID
secret_access_key = $RCLONE_CONFIG_MISSIS_SECRET_ACCESS_KEY
endpoint = $RCLONE_CONFIG_MISSIS_ENDPOINT
region = auto
bucket = $RCLONE_CONFIG_MISSIS_BUCKET
EOF
  remote_path="${remote_dest%/}"
  if [[ "$remote_path" == *: ]]; then
    remote_target="$remote_path$store_id/$head_hash.db"
  else
    remote_target="$remote_path/$store_id/$head_hash.db"
  fi
  rclone --config "$tmpconfig" copyto "$backup" "$remote_target"
  echo "uploaded with rclone"
  exit 0
fi

if command -v aws >/dev/null 2>&1; then
  bucket="${MISSIS_S3_BUCKET:-}"
  if [ -z "$bucket" ]; then
    echo "MISSIS_S3_BUCKET is not set" >&2
    exit 1
  fi
  aws s3 cp "$backup" "s3://$bucket/missis-backups/$store_id/$head_hash.db"
  echo "uploaded with aws"
  exit 0
fi

echo "no remote upload target configured" >&2
exit 1
