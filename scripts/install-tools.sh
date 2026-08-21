#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

bash "$root/scripts/install.sh" --tools-only

if [[ "${MISSIS_INSTALL_LEGACY_TOOLS:-0}" == "1" ]]; then
	module="github.com/ravinsharma7/missis"
	ref="${MISSIS_REF:-latest}"
	if [[ -n "${MISSIS_BIN_DIR:-}" ]]; then
		bin_dir="$MISSIS_BIN_DIR"
	elif [[ -n "${GOBIN:-}" && ":$PATH:" == *":$GOBIN:"* ]]; then
		bin_dir="$GOBIN"
	else
		bin_dir="$(go env GOPATH)/bin"
	fi
	case ":$PATH:" in
	*":$bin_dir:"*)
		;;
	*)
		echo "legacy tool install directory is not on PATH: $bin_dir" >&2
		exit 1
		;;
	esac
	for tool in ticket-tui repair-store store-gaps store-manifest store-backup store-remote; do
		echo "installing legacy $tool@$ref to $bin_dir"
		GOBIN="$bin_dir" go install "$module/tools/$tool@$ref"
	done
fi
