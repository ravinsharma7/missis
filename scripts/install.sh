#!/usr/bin/env bash
set -euo pipefail

module="github.com/ravinsharma7/missis"
ref="${MISSIS_REF:-}"
install_source="${MISSIS_INSTALL_SOURCE:-release}"

if [[ -z "$ref" ]]; then
	echo "MISSIS_REF must be explicit (use a stable tag for release installation)" >&2
	exit 2
fi

if [[ "$#" -ne 0 ]]; then
	echo "usage: scripts/install.sh" >&2
	exit 2
fi

goos="$(go env GOOS)"
goexe="$(go env GOEXE)"
case "$goos" in
linux|darwin|freebsd|openbsd|netbsd)
	;;
*)
	echo "POSIX installer requires a POSIX Go target; got GOOS=$goos GOEXE=$goexe" >&2
	echo "Use scripts/install.ps1 from native Windows instead." >&2
	exit 1
	;;
esac

if [[ -n "${MISSIS_BIN_DIR:-}" ]]; then
	bin_dir="$MISSIS_BIN_DIR"
elif [[ -n "${GOBIN:-}" ]]; then
	bin_dir="$GOBIN"
	case ":$PATH:" in
	*":$bin_dir:"*)
		;;
	*)
		echo "GOBIN=$GOBIN is not on PATH; using $(go env GOPATH)/bin instead" >&2
		bin_dir="$(go env GOPATH)/bin"
		;;
	esac
else
	bin_dir="$(go env GOPATH)/bin"
fi

mkdir -p "$bin_dir"
bin_dir="$(cd "$bin_dir" && pwd)"
case ":$PATH:" in
*":$bin_dir:"*)
	;;
*)
	echo "install directory is not on PATH: $bin_dir" >&2
	echo "export PATH=\"$bin_dir:\$PATH\" and rerun this installer" >&2
	exit 1
	;;
esac

install_package() {
	local package="$1"
	echo "installing $module/$package@$ref to $bin_dir"
	GOBIN="$bin_dir" go install "$module/$package@$ref"
}

if [[ "$install_source" != "go" ]]; then
	echo "installing verified paired release $ref to $bin_dir"
	go run "$module/tools/paired-install@$ref" --ref "$ref" --bin-dir "$bin_dir"
else
	install_package cmd/missis
	install_package tools/missis-tools
fi

verify_binary() {
	local name="$1"
	local path="$bin_dir/$name$goexe"
	if [[ ! -x "$path" ]]; then
		echo "installed binary is missing or not executable: $path" >&2
		exit 1
	fi
	if ! command -v file >/dev/null 2>&1; then
		echo "cannot verify native binary format: the file command is required" >&2
		exit 1
	fi
	local format
	format="$(file -b "$path")"
	case "$goos:$format" in
	linux:*ELF*|freebsd:*ELF*|openbsd:*ELF*|netbsd:*ELF*|darwin:*Mach-O*)
		;;
	*)
		echo "non-native binary at $path: $format" >&2
		exit 1
		;;
	esac
	local resolved
	resolved="$(command -v "$name" || true)"
	if [[ "$resolved" != "$path" ]]; then
		echo "command resolution selected ${resolved:-<missing>} instead of $path" >&2
		exit 1
	fi
}

verify_binary missis
verify_binary missis-tools

if [[ "$install_source" == "go" ]]; then
	echo "source/module installation is not eligible for verified self-update; use a stable paired release" >&2
fi

echo "installed native $goos Missis binaries in $bin_dir"
echo "missis: $(command -v missis)"
echo "missis-tools: $(command -v missis-tools)"
echo "ref: $ref"
