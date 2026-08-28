#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

lock="specs/event-tooling.lock.json"
jq -e '
  .version == "missis-event-tooling-lock-v1"
  and .authority_repository == "git@github.com:ravinsharma7/skunkwork.git"
  and (.authority_commit | test("^[0-9a-f]{40}$"))
  and .protocol == "eventstore-v3-alpha.5"
  and (.snapshots | length) == 2
' "$lock" >/dev/null

while IFS=$'\t' read -r path expected; do
  # Native jq on Windows terminates TSV records with CRLF. Bash read removes
  # LF but retains CR on the final field; it is transport, not lock content.
  expected="${expected%$'\r'}"
  if [ ! -f "$path" ]; then
    echo "event-tooling authority lock: snapshot missing: $path" >&2
    exit 1
  fi
  actual="$(sha256sum "$path" | cut -d' ' -f1)"
  if [ "$actual" != "$expected" ]; then
    echo "event-tooling authority lock: digest mismatch: $path" >&2
    echo "expected: $expected" >&2
    echo "actual:   $actual" >&2
    exit 1
  fi
done < <(jq -r '.snapshots[] | [.local_path, .sha256] | @tsv' "$lock")

echo "event-tooling authority lock verified"
