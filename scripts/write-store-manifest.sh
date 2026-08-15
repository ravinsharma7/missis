#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
mkdir -p issues/missis-store
MISSIS_STORE=".missis-store/missis.db" go run ./tools/store-manifest > issues/missis-store/manifest.json
