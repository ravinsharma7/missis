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
bucket="${MISSIS_RCLONE_BUCKET:-}"
if [ -z "$bucket" ]; then
  echo "MISSIS_RCLONE_BUCKET is not set" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir" "$tmpconfig"' EXIT
dest="$tmpdir/$head_hash.db"

tmpconfig="$(mktemp)"
cat > "$tmpconfig" <<EOF
[missis]
type = s3
provider = Cloudflare
access_key_id = $RCLONE_CONFIG_MISSIS_ACCESS_KEY_ID
secret_access_key = $RCLONE_CONFIG_MISSIS_SECRET_ACCESS_KEY
endpoint = $RCLONE_CONFIG_MISSIS_ENDPOINT
region = auto
EOF

remote_path="${remote%/}"
if [[ "$remote_path" == *: ]]; then
  remote_root="${remote_path}${bucket}"
else
  remote_root="${remote_path}"
fi
remote_source="${remote_root}/${store_id}/${head_hash}.db"
rclone --config "$tmpconfig" --s3-no-check-bucket copyto "$remote_source" "$dest"

restored="$(MISSIS_STORE="$dest" go run ./tools/store-manifest)"
restored_id="$(printf '%s' "$restored" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
restored_head="$(printf '%s' "$restored" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"

if [ "$restored_id" != "$store_id" ] || [ "$restored_head" != "$head_hash" ]; then
  echo "downloaded backup verification failed" >&2
  exit 1
fi

echo "downloaded backup verified"
