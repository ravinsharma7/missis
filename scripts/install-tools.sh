#!/usr/bin/env bash
set -euo pipefail

module="github.com/ravinsharma7/missis"
tools=(
  ticket-tui
  repair-store
  store-gaps
  store-manifest
  store-backup
)

for tool in "${tools[@]}"; do
  echo "installing $tool"
  go install "$module/tools/$tool@latest"
done
