#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(git rev-parse --show-toplevel)"
target="$repo_dir/.git/hooks/pre-commit"
source_hook="$repo_dir/scripts/pre-commit"

if [ -e "$target" ]; then
	echo "refusing to overwrite existing hook: $target" >&2
	exit 1
fi
cp "$source_hook" "$target"
chmod +x "$target"
echo "installed verify-only pre-commit hook: $target"
