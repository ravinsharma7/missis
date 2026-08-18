#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [ "$#" -lt 1 ]; then
  echo "usage: download-backup <destination>" >&2
  exit 2
fi

go run ./tools/store-remote download "$1"
