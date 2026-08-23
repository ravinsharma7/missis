#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

fakebin="$tmpdir/fakebin"
gopath="$tmpdir/gopath"
mkdir -p "$fakebin" "$gopath/bin"

cat > "$fakebin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "env" ]]; then
  case "$2" in
    GOOS) echo linux ;;
    GOEXE) echo ;;
    GOPATH) echo "$FAKE_GOPATH" ;;
    *) exit 1 ;;
  esac
  exit 0
fi
if [[ "$1" == "install" ]]; then
  package="${!#}"
  case "$package" in
    */tools/missis-tools@*) name=missis-tools ;;
    */cmd/missis@*) name=missis ;;
    *) echo "unexpected install package: $package" >&2; exit 1 ;;
  esac
  : "${GOBIN:?GOBIN must be set by installer}"
  mkdir -p "$GOBIN"
  touch "$GOBIN/$name"
  chmod +x "$GOBIN/$name"
  exit 0
fi
echo "unexpected go invocation: $*" >&2
exit 1
EOF

cat > "$fakebin/file" <<'EOF'
#!/usr/bin/env bash
echo "ELF 64-bit LSB executable"
EOF

chmod +x "$fakebin/go" "$fakebin/file"

output="$tmpdir/output"
FAKE_GOPATH="$gopath" \
PATH="$gopath/bin:$fakebin:$PATH" \
MISSIS_BIN_DIR='' \
MISSIS_INSTALL_SOURCE=go \
GOBIN="$tmpdir/not-on-path" \
bash "$root/scripts/install.sh" >"$output" 2>"$tmpdir/error"

test -x "$gopath/bin/missis"
test -x "$gopath/bin/missis-tools"
grep -q "GOBIN=.*not on PATH" "$tmpdir/error"
grep -q "missis-tools: $gopath/bin/missis-tools" "$output"

echo "installer path and native-format checks passed"
