#!/usr/bin/env bash
set -euo pipefail

# Pinned so local and hosted validation use the same parser and rule set.
actionlint_version="v1.7.12"

go run "github.com/rhysd/actionlint/cmd/actionlint@${actionlint_version}" \
  .github/workflows/ci.yml \
  .github/workflows/release.yml
