#!/usr/bin/env bash
set -euo pipefail

module="github.com/ravinsharma7/missis"
ref="${MISSIS_REF:-latest}"

echo "installing missis-tools"
go install "$module/tools/missis-tools@$ref"

if [[ "${MISSIS_INSTALL_LEGACY_TOOLS:-0}" == "1" ]]; then
  for tool in ticket-tui repair-store store-gaps store-manifest store-backup store-remote; do
    echo "installing legacy $tool"
    go install "$module/tools/$tool@$ref"
  done
fi
