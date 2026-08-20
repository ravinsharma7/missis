#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
real_go="$(command -v go)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/scripts" "$tmp/backups" "$tmp/bin"
cp "$repo_root/scripts/upload-backup.sh" "$tmp/scripts/upload-backup.sh"
cp "$repo_root/scripts/download-backup.sh" "$tmp/scripts/download-backup.sh"

store_path="$tmp/store.db"
backup_path=""

(
  cd "$repo_root"
  "$real_go" run ./cmd/missis new --store "$store_path" "rclone backup fixture" >/dev/null
  backup_path="$tmp/backups/fixture.db"
  MISSIS_STORE="$store_path" "$real_go" run ./tools/missis-tools backup "$backup_path"
  printf '%s\n' "$backup_path" > "$tmp/backup-path"
)
backup_path="$(<"$tmp/backup-path")"
manifest_json="$(
  cd "$repo_root"
  "$real_go" run ./tools/missis-tools manifest "$store_path"
)"
store_id="$(printf '%s\n' "$manifest_json" | sed -n 's/.*"store_id": "\([^"]*\)".*/\1/p')"
head_hash="$(printf '%s\n' "$manifest_json" | sed -n 's/.*"head_hash": "\([^"]*\)".*/\1/p')"

if [ -z "$store_id" ] || [ -z "$head_hash" ]; then
  echo "failed to read fixture manifest" >&2
  exit 1
fi
expected_backup="$tmp/backups/${store_id//:/_}-${head_hash}.db"
mv "$backup_path" "$expected_backup"
backup_path="$expected_backup"
"$real_go" -C "$repo_root" build -o "$tmp/bin/missis-tools" ./tools/missis-tools

cat > "$tmp/bin/rclone" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
config=""
no_check=0
source=""
dest=""
args=("$@")
is_size=0
for i in "${!args[@]}"; do
  case "${args[$i]}" in
    --config) config="${args[$((i+1))]}" ;;
    --s3-no-check-bucket) no_check=1 ;;
    size) is_size=1 ;;
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
if [ "$is_size" = 1 ]; then
  echo "directory not found" >&2
  exit 1
fi
if [[ "$source" == missis:* ]]; then
  cp "${FAKE_RESTORE_SOURCE:?FAKE_RESTORE_SOURCE is not set}" "$dest"
fi
EOF
chmod +x "$tmp/bin/rclone"

cat > "$tmp/bin/go" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [ "\${1:-}" != run ] || [ "\${2:-}" != ./tools/missis-tools ]; then
  echo "unexpected go invocation: \$*" >&2
  exit 1
fi
shift 2
if [ "\${1:-}" != remote ]; then
  echo "unexpected missis-tools invocation: \$*" >&2
  exit 1
fi
shift
if [ "\${1:-}" = upload ]; then
  set -- remote upload "$backup_path"
else
  set -- remote "\$@"
fi
exec "$tmp/bin/missis-tools" "\$@"
EOF
chmod +x "$tmp/bin/go"

export PATH="$tmp/bin:$PATH"
export MISSIS_STORE="$store_path"
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
  if grep -Eq '/(latest|production)\.db$' "$log"; then
    echo "unexpected human-readable fixed key in $name" >&2
    return 1
  fi
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
    bash scripts/download-backup.sh "$tmp/restored.db"
  )"
  echo "download_case=$name" >> "$log"
  grep -q 'no_check=1' "$log"
  grep -q "source=missis:test-bucket/${store_id}/${head_hash}.db" "$log"
  if grep -Eq '/(latest|production)\.db$' "$log"; then
    echo "unexpected human-readable fixed key in $name" >&2
    return 1
  fi
  if grep -q '^bucket =' "$log"; then
    echo "unexpected rclone bucket config in $name" >&2
    return 1
  fi
  printf '%s\n' "$output" | grep -q 'downloaded backup verified'
}

export MISSIS_RCLONE_BUCKET="test-bucket"
run_upload_case "required_bucket_variable" "$tmp/upload.log"
run_download_case "required_bucket_variable" "$tmp/download.log"

unset MISSIS_RCLONE_BUCKET
if (cd "$tmp" && bash scripts/upload-backup.sh >/dev/null 2>&1); then
  echo "expected upload to fail without MISSIS_RCLONE_BUCKET" >&2
  exit 1
fi
if (cd "$tmp" && bash scripts/download-backup.sh >/dev/null 2>&1); then
  echo "expected download to fail without MISSIS_RCLONE_BUCKET" >&2
  exit 1
fi

echo "rclone backup script tests passed"
