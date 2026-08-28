#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

identity="$(go run ./cmd/missis --version --json)"
store_format="$(jq -r '.store_format_revision' <<<"$identity")"
normal_format="$(jq -r '.normal_open_format' <<<"$identity")"
migratable="$(jq -c '.migratable_from_formats' <<<"$identity")"

test "$store_format" = "$normal_format"
test "$store_format" -gt 0

grep -Fq "\$missis.store_format_revision -ne $store_format" scripts/ci/verify-windows.ps1
grep -Fq "\$tools.store_format_revision -ne $store_format" scripts/ci/verify-windows.ps1
grep -Fq "\$missis.normal_open_format -ne $normal_format" scripts/ci/verify-windows.ps1
grep -Fq "\$tools.normal_open_format -ne $normal_format" scripts/ci/verify-windows.ps1
grep -Fq ".store_format_revision == $store_format and .normal_open_format == $normal_format and .migratable_from_formats == $migratable" .github/workflows/release.yml
grep -Fq "store_format=$store_format" .github/workflows/release.yml
grep -Fq ".normal_open_format == $normal_format and .migratable_from_formats == $migratable" testsuite/scripts/test-published-install.sh
grep -Fq "store_format=$store_format" testsuite/scripts/test-published-install.sh

echo "store-format declarations agree at revision $store_format"
