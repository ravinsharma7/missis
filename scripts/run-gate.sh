#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

echo "== model and store tests =="
go test ./implementation/...

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
bin="$tmpdir/missis"

echo "== build missis =="
go build -o "$bin" ./cmd/missis

echo "== black-box tests =="
MISSIS_BIN="$bin" go test ./testsuite/blackbox

echo "== Phase 1 MUST register =="
grep -E '^\| PH1-' specs/phase1-requirements.md || true

echo "== Phase 1 SHOULD backlog =="
grep -E '^\| N[0-9]+ ' issues/phase1-should-backlog.md || true

echo "gate: PASS"
