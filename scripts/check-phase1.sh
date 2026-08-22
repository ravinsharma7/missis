#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

echo "== model and store tests =="
go test ./...

echo "== rclone backup script tests =="
bash testsuite/scripts/test-rclone-backup.sh

echo "== manifest freshness script tests =="
bash testsuite/scripts/test-manifest-freshness.sh

echo "== benchmark selection tests =="
bash testsuite/benchmarks/test-benchmark-selection.sh

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
bin="$tmpdir/missis"
repair_bin="$tmpdir/repair-store"

echo "== build missis =="
go build -o "$bin" ./cmd/missis
go build -o "$repair_bin" ./tools/repair-store

echo "== black-box tests =="
MISSIS_BIN="$bin" MISSIS_REPAIR_BIN="$repair_bin" go test ./testsuite/blackbox

required_file="$tmpdir/required.txt"
phase1_requirements="${MISSIS_PHASE1_REQUIREMENTS_PATH:-specs/phase1-requirements.md}"
should_backlog="${MISSIS_SHOULD_BACKLOG_PATH:-specs/phase1-should-backlog.md}"
if [ ! -f "$should_backlog" ] && [ -f ".missis.d/phase1-should-backlog.md" ]; then
  echo "warning: using legacy .missis.d/phase1-should-backlog.md; migrate it to specs/" >&2
  should_backlog=".missis.d/phase1-should-backlog.md"
fi
grep -E '^\| PH1-' "$phase1_requirements" | sed -E 's/^\| ([^ ]+) .*/\1/' > "$required_file"
awk -F'|' '/^\| N[0-9]+ / {
  id=$2; decision=$3;
  gsub(/ /, "", id); gsub(/ /, "", decision);
  if (decision == "adopt") print id;
}' "$should_backlog" >> "$required_file"

coverage_manifest="$tmpdir/coverage.txt"
go run ./tools/coverage > "$coverage_manifest"

missing=0
while IFS= read -r id; do
  id="$(printf '%s' "$id" | tr -d '[:space:]')"
  [ -z "$id" ] && continue
  if ! grep -qF -- "$id" "$coverage_manifest"; then
    echo "missing coverage: $id"
    missing=1
  fi
done < "$required_file"

echo "== required IDs =="
tr '\n' ' ' < "$required_file"
echo

if [ "$missing" -ne 0 ]; then
  echo "traceability: FAIL"
  exit 1
fi

echo "traceability: PASS"
