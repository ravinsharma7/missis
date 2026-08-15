#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/scripts" "$tmp/issues/missis-store" "$tmp/backups" "$tmp/bin"
cp "$repo_root/scripts/upload-backup.sh" "$tmp/scripts/upload-backup.sh"
cp "$repo_root/scripts/download-backup.sh" "$tmp/scripts/download-backup.sh"

store_id="store:TESTSTOREID"
head_hash="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
backup_basename="${store_id//:/_}-${head_hash}.db"
backup_path="$tmp/backups/$backup_basename"
manifest_path="$tmp/issues/missis-store/manifest.json"

printf 'dummy backup\n' > "$backup_path"
cat > "$manifest_path" <<EOF
{
  "store_id": "$store_id",
  "head_hash": "$head_hash",
  "event_count": 1,
  "schema_version": "test"
}
EOF

cat > "$tmp/bin/rclone" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
config=""
no_check=0
source=""
dest=""
args=("$@")
for i in "${!args[@]}"; do
  case "${args[$i]}" in
    --config) config="${args[$((i+1))]}" ;;
    --s3-no-check-bucket) no_check=1 ;;
    copyto)
      source="${args[$((i+1))]}"
      dest="${args[$((i+2))]}"
      ;;
  esac
done
{
  echo "no_check=$no_check"
  echo "source=$source"
  echo "dest=$dest"
  if [ -n "$config" ]; then
    echo "config:"
    sed -E 's/(access_key_id|secret_access_key) = .*/\1 = <redacted>/' "$config"
  fi
} >> "${RCLONE_TEST_LOG:?RCLONE_TEST_LOG is not set}"
if [[ "$source" == missis:* ]]; then
  cp "${FAKE_RESTORE_SOURCE:?FAKE_RESTORE_SOURCE is not set}" "$dest"
fi
EOF
chmod +x "$tmp/bin/rclone"

cat > "$tmp/bin/go" <<EOF
#!/usr/bin/env bash
cat <<'JSON'
{
  "store_id": "$store_id",
  "head_hash": "$head_hash",
  "event_count": 1,
  "schema_version": "test"
}
JSON
EOF
chmod +x "$tmp/bin/go"

export PATH="$tmp/bin:$PATH"
export MISSIS_RCLONE_REMOTE="missis:"
export RCLONE_CONFIG_MISSIS_TYPE="s3"
export RCLONE_CONFIG_MISSIS_PROVIDER="Cloudflare"
export RCLONE_CONFIG_MISSIS_ACCESS_KEY_ID="test-access-key-id"
export RCLONE_CONFIG_MISSIS_SECRET_ACCESS_KEY="test-secret-access-key"
export RCLONE_CONFIG_MISSIS_ENDPOINT="https://example.r2.cloudflarestorage.com"
export FAKE_RESTORE_SOURCE="$backup_path"

run_upload_case() {
  local name="$1"
  local log="$2"
  export RCLONE_TEST_LOG="$log"
  (
    cd "$tmp"
    bash scripts/upload-backup.sh >/dev/null
  )
  echo "upload_case=$name" >> "$log"
  grep -q 'no_check=1' "$log"
  grep -q "dest=missis:test-bucket/${store_id}/${head_hash}.db" "$log"
  if grep -q '^bucket =' "$log"; then
    echo "unexpected rclone bucket config in $name" >&2
    return 1
  fi
}

run_download_case() {
  local name="$1"
  local log="$2"
  export RCLONE_TEST_LOG="$log"
  output="$(
    cd "$tmp"
    bash scripts/download-backup.sh
  )"
  echo "download_case=$name" >> "$log"
  grep -q 'no_check=1' "$log"
  grep -q "source=missis:test-bucket/${store_id}/${head_hash}.db" "$log"
  if grep -q '^bucket =' "$log"; then
    echo "unexpected rclone bucket config in $name" >&2
    return 1
  fi
  printf '%s\n' "$output" | grep -q 'downloaded backup verified'
}

export MISSIS_RCLONE_BUCKET="test-bucket"
run_upload_case "new_bucket_variable" "$tmp/upload-new.log"
run_download_case "new_bucket_variable" "$tmp/download-new.log"

unset MISSIS_RCLONE_BUCKET
export RCLONE_CONFIG_MISSIS_BUCKET="test-bucket"
run_upload_case "legacy_bucket_variable" "$tmp/upload-legacy.log"
run_download_case "legacy_bucket_variable" "$tmp/download-legacy.log"

echo "rclone backup script tests passed"
