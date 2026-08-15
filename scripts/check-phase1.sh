#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

echo "== model and store tests =="
go test ./...

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
bin="$tmpdir/missis"

echo "== build missis =="
go build -o "$bin" ./cmd/missis

echo "== black-box tests =="
MISSIS_BIN="$bin" go test ./testsuite/blackbox

required_file="$tmpdir/required.txt"
grep -E '^\| PH1-' specs/phase1-requirements.md | sed -E 's/^\| ([^ ]+) .*/\1/' > "$required_file"
awk -F'|' '/^\| N[0-9]+ / {
  id=$2; decision=$3;
  gsub(/ /, "", id); gsub(/ /, "", decision);
  if (decision == "adopt") print id;
}' issues/phase1-should-backlog.md >> "$required_file"

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
