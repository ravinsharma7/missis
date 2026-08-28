#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

lock="specs/event-tooling.lock.json"
jq -e '
  .version == "missis-event-tooling-lock-v1"
  and .authority_repository == "git@github.com:ravinsharma7/skunkwork.git"
  and (.authority_commit | test("^[0-9a-f]{40}$"))
  and .protocol == "eventstore-v3-alpha.3"
  and (.snapshots | length) == 2
' "$lock" >/dev/null

while IFS=$'\t' read -r path expected; do
  test -f "$path"
  actual="$(sha256sum "$path" | cut -d' ' -f1)"
  test "$actual" = "$expected"
done < <(jq -r '.snapshots[] | [.local_path, .sha256] | @tsv' "$lock")

echo "event-tooling authority lock verified"
