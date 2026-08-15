#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
store_path="${MISSIS_STORE:-.missis-store/missis.db}"
manifest_path="${MISSIS_MANIFEST_PATH:-.missis.d/manifest.json}"
mkdir -p "$(dirname "$manifest_path")"
MISSIS_STORE="$store_path" go run ./tools/store-manifest > "$manifest_path"
