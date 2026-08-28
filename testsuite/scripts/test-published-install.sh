#!/usr/bin/env bash
set -euo pipefail

# Run this against a selected release ref. By default it downloads the public
# release manifest; release automation may provide MISSIS_MANIFEST_URL to smoke
# the exact staged artifacts before promoting a draft. It deliberately installs
# into a temporary GOBIN so it cannot alter the operator's existing Go tools.
module="github.com/ravinsharma7/missis"
ref="${MISSIS_REF:?MISSIS_REF must name a published release containing --setup}"
# A draft release has no fetchable Git tag. Release automation therefore runs
# the installer source from the exact tested commit while still requiring the
# requested stable ref in the artifact manifest. Normal published installs use
# the stable ref for both identities.
module_ref="${MISSIS_MODULE_REF:-$ref}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export GOBIN="$tmp/bin"
mkdir -p "$GOBIN" "$tmp/project"
unset MISSIS_STORE || true

export PATH="$GOBIN:$PATH"
install_args=(--ref "$ref" --bin-dir "$GOBIN" --project "$tmp/project" --json)
if [ -n "${MISSIS_MANIFEST_URL:-}" ]; then
  install_args+=(--manifest-url "$MISSIS_MANIFEST_URL")
fi
go run "$module/tools/paired-install@$module_ref" "${install_args[@]}" >/dev/null

cd "$tmp/project"
test -x "$GOBIN/missis"
test -x "$GOBIN/missis-tools"
missis --version | grep -Eq "missis version=${ref}\\+g[0-9a-f]{12} commit=[0-9a-f]{40} store_format=6"
missis-tools --version | grep -Eq "missis-tools version=${ref}\\+g[0-9a-f]{12} commit=[0-9a-f]{40} store_format=6"
missis --version --json | jq -e '.normal_open_format == 6 and .migratable_from_formats == [1,2,3,4,5,6] and (.migration_set_digest | test("^[0-9a-f]{64}$"))' >/dev/null
missis-tools --version --json | jq -e '.normal_open_format == 6 and .migratable_from_formats == [1,2,3,4,5,6] and (.migration_set_digest | test("^[0-9a-f]{64}$"))' >/dev/null
missis-tools --help | grep -q "backup verify"
missis --setup --project . --json >/dev/null
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
