#!/usr/bin/env bash
set -euo pipefail

# Run this after the selected ref has been published. It deliberately installs
# into a temporary GOBIN so it cannot alter the operator's existing Go tools.
module="github.com/ravinsharma7/missis"
ref="${MISSIS_REF:-v0.2.0}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export GOBIN="$tmp/bin"
mkdir -p "$GOBIN" "$tmp/project"
unset MISSIS_STORE || true

go install "$module/cmd/missis@$ref"
go install "$module/tools/missis-tools@$ref"
export PATH="$GOBIN:$PATH"

cd "$tmp/project"
missis --init --json >/dev/null
missis show --health >/dev/null
missis-tools manifest >/dev/null
missis-tools gaps .missis-store/missis.db >/dev/null

echo "published install smoke passed for $ref"
