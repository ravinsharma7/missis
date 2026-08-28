#!/usr/bin/env bash
set -euo pipefail

mode="${1:-}"
case "$mode" in
  linux|windows|release) ;;
  *) echo "usage: scripts/ci/verify.sh <linux|windows|release>" >&2; exit 2 ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

go vet ./...
go test -count=1 ./...
go run ./tools/generate-onboarding --check
go run ./tools/coverage --registry specs/requirements-registry.v3.json
go build ./tools/missis-tools
bash scripts/check-event-tooling-lock.sh
bash scripts/check-format-declarations.sh

# Workflow syntax is platform-independent and is linted once by Linux and
# release verification. actionlint's Go runner exits without diagnostics under
# hosted Windows Git Bash, so it is not part of the native Windows surface.
if [ "$mode" != windows ]; then
  bash scripts/check-workflows.sh
fi

case "$mode" in
  linux)
    go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...
    command -v shellcheck >/dev/null || {
      echo "shellcheck is required for Linux product verification" >&2
      exit 1
    }
    shellcheck scripts/*.sh scripts/ci/*.sh testsuite/scripts/*.sh testsuite/benchmarks/*.sh
    bash testsuite/scripts/test-install.sh
    bash testsuite/scripts/test-rclone-backup.sh
    bash testsuite/benchmarks/test-benchmark-selection.sh
    ;;
  windows)
    # Native PowerShell syntax and paired-binary checks run separately.
    ;;
  release)
    bash testsuite/scripts/test-install.sh
    bash testsuite/scripts/test-rclone-backup.sh
    bash testsuite/benchmarks/test-benchmark-selection.sh
    ;;
esac

echo "Missis product verification passed: $mode"
