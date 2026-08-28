#!/usr/bin/env bash
set -euo pipefail

product_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
distribution_dir="$product_root/third_party/eventstore"
lock_path="$product_root/specs/eventstore-package.lock.json"

actual_files="$(cd "$distribution_dir" && find . -maxdepth 1 -type f -printf '%P\n' | sort)"
[[ "$actual_files" == $'eventstore.go\ngo.mod' ]] || {
  echo "unexpected generated eventstore files:" >&2
  printf '%s\n' "$actual_files" >&2
  exit 1
}

jq -e '
  .version == "eventstore-package-distribution-v1"
  and .package_version == "eventstore-go-v0.1.0-alpha.1"
  and .module == "github.com/ravinsharma7/skunkwork/packages/eventstore"
  and .generator == "scripts/sync-eventstore-package.sh@v1"
  and [.files[].path] == ["go.mod", "eventstore.go"]
  and all(.files[]; (.sha256 | test("^[0-9a-f]{64}$")))
  and (keys | sort) == (["files", "generator", "module", "package_version", "version"] | sort)
' "$lock_path" >/dev/null

while IFS=$'\t' read -r relative expected; do
  file="$distribution_dir/$relative"
  [[ -f "$file" ]] || { echo "eventstore distribution file missing: $file" >&2; exit 1; }
  actual="$(sha256sum "$file" | cut -d' ' -f1)"
  [[ "$actual" == "$expected" ]] || {
    echo "eventstore distribution digest mismatch: $relative expected=$expected actual=$actual" >&2
    exit 1
  }
done < <(jq -r '.files[] | [.path, .sha256] | @tsv' "$lock_path")

module="$(sed -n 's/^module //p' "$distribution_dir/go.mod")"
[[ "$module" == "github.com/ravinsharma7/skunkwork/packages/eventstore" ]] || {
  echo "unexpected generated eventstore module: $module" >&2
  exit 1
}

dependencies="$(cd "$distribution_dir" && go list -deps ./...)"
if grep -Eq 'github.com/ravinsharma7/missis|modernc.org/sqlite' <<<"$dependencies"; then
  echo "neutral eventstore package imports a Missis or SQLite dependency" >&2
  exit 1
fi

imports="$(cd "$distribution_dir" && go list -f '{{range .Imports}}{{println .}}{{end}}' . | sort)"
expected_imports=$'bytes\ncontext\nencoding/json\nerrors\nfmt\nio\nstrings\ntime'
[[ "$imports" == "$expected_imports" ]] || {
  echo "neutral eventstore import boundary changed:" >&2
  printf '%s\n' "$imports" >&2
  exit 1
}

if grep -R -n --include='*.go' '"github.com/ravinsharma7/missis/pkg/eventstore"' \
  "$product_root/internal" "$product_root/cmd" "$product_root/tools"; then
  echo "Missis implementation still imports the compatibility eventstore path" >&2
  exit 1
fi

canonical_dir="$product_root/../../packages/eventstore"
if [[ -d "$canonical_dir" ]]; then
  cmp -s "$canonical_dir/go.mod" "$distribution_dir/go.mod" || {
    echo "generated eventstore go.mod differs from canonical source" >&2
    exit 1
  }
  cmp -s "$canonical_dir/eventstore.go" "$distribution_dir/eventstore.go" || {
    echo "generated eventstore source differs from canonical source" >&2
    exit 1
  }
fi

echo "eventstore generated distribution verified"
