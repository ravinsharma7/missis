#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

for file in testsuite/blackbox/*_test.go implementation/model/*_test.go implementation/store/*_test.go; do
  awk -v file="$file" '
    /^func Test/ {
      test=$2
      sub(/\(.*/, "", test)
      line=NR
      ids=""
      next
    }
    /covers / {
      for (i=1; i<=NF; i++) {
        if ($i ~ /^(PH1-|N[0-9])/) ids = ids " " $i
      }
      if (test != "") {
        print file ":" line ":" test ":" ids
        test=""
      }
    }
  ' "$file"
done
