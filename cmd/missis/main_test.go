package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFakeGo(t *testing.T, dir, listJSON string, installExit int) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "list" ]; then
  cat <<'JSON'
` + listJSON + `
JSON
elif [ "$1" = "install" ]; then
  exit ` + string(rune('0'+installExit)) + `
fi
exit 0
`
	path := filepath.Join(dir, "go")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLatestModuleVersionHermetic(t *testing.T) {
	dir := t.TempDir()
	writeFakeGo(t, dir, `{"Version":"vTest","Time":"2026-01-01T00:00:00Z"}`, 0)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	info, err := latestModuleVersion()
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "vTest" {
		t.Fatalf("version = %q, want vTest", info.Version)
	}
}

func TestSelfUpdateHermetic(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The hermetic shim fakes `go` with a POSIX #!/bin/sh script; skip on
		// Windows. A build-tagged go.exe shim is the alternative if Windows
		// coverage is wanted later (ticket #55).
		t.Skip("self-update hermetic test uses a POSIX sh shim")
	}
	dir := t.TempDir()
	writeFakeGo(t, dir, `{"Version":"vTest","Time":"2026-01-01T00:00:00Z"}`, 0)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if code := runSelfUpdate(false); code != exitSuccess {
		t.Fatalf("self update code = %d", code)
	}
}

func TestStorePermissionWarningsPOSIXScoped(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got := storePermissionWarnings(t.TempDir()); len(got) != 0 {
			t.Fatalf("expected no permission warnings on windows, got %v", got)
		}
		return
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if got := storePermissionWarnings(dir); len(got) == 0 {
		t.Fatal("expected permission warning on POSIX for world-writable dir")
	}
}
