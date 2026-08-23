#!/usr/bin/env bash
set -euo pipefail

# Run this after the selected ref has been published. It deliberately installs
# into a temporary GOBIN so it cannot alter the operator's existing Go tools.
module="github.com/ravinsharma7/missis"
ref="${MISSIS_REF:-v0.2.2}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export GOBIN="$tmp/bin"
mkdir -p "$GOBIN" "$tmp/project"
unset MISSIS_STORE || true

export PATH="$GOBIN:$PATH"
go run "$module/tools/paired-install@$ref" --ref "$ref" --bin-dir "$GOBIN"

cd "$tmp/project"
test -x "$GOBIN/missis"
test -x "$GOBIN/missis-tools"
missis --version | grep -Eq "missis version=${ref}\\+g[0-9a-f]{12} commit=[0-9a-f]{40} store_format=2"
missis-tools --version | grep -Eq "missis-tools version=${ref}\\+g[0-9a-f]{12} commit=[0-9a-f]{40} store_format=2"
missis-tools --help | grep -q "backup verify"
missis --init --json >/dev/null
missis show --health >/dev/null
ticket="$(missis new --json "published install smoke")"
ref_value="$(printf '%s' "$ticket" | sed -n 's/.*"ref":"\([^"]*\)".*/\1/p')"
test -n "$ref_value"
missis set --json "$ref_value/smoke" "installed workflow" --kind text >/dev/null
missis show --json "$ref_value" >/dev/null
missis-tools backup "$tmp/published-backup.db"
missis-tools backup verify "$tmp/published-backup.db" >/dev/null
missis-tools manifest >/dev/null
missis-tools gaps .missis-store/missis.db >/dev/null

echo "published install smoke passed for $ref"
