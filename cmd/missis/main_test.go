package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if runtime.GOOS == "windows" {
		// Same POSIX sh shim as TestSelfUpdateHermetic (ticket #55).
		t.Skip("hermetic go shim is POSIX-only")
	}
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

func TestCommitLabelExplainsUnknown(t *testing.T) {
	note := commitUnknownNote("v0.1.0")
	labeled := commitLabel(unknownCommit, note)
	if !strings.Contains(labeled, "module download") {
		t.Fatalf("unknown commit label = %q, want explanation", labeled)
	}
	if got := commitLabel("1da0b1f57715", note); got != "1da0b1f57715" {
		t.Fatalf("known commit label = %q, want unchanged", got)
	}
}

func TestCommitUnknownNoteDistinguishesBuildKinds(t *testing.T) {
	moduleNote := commitUnknownNote("v0.1.0")
	if !strings.Contains(moduleNote, "module download") {
		t.Fatalf("module-version note = %q, want module download cause", moduleNote)
	}
	for _, mainVersion := range []string{"", "(devel)"} {
		sourceNote := commitUnknownNote(mainVersion)
		if !strings.Contains(sourceNote, "source tree") {
			t.Fatalf("source-build note for %q = %q, want source-tree cause", mainVersion, sourceNote)
		}
		if strings.Contains(sourceNote, "module download") {
			t.Fatalf("source-build note for %q must not claim a module download: %q", mainVersion, sourceNote)
		}
	}
	if got := commitUnknownNote("v0.1.1-0.20260819052312-1da0b1f57715+dirty"); !strings.Contains(got, "module download") {
		t.Fatalf("pseudo-version note = %q, want module download cause", got)
	}
}

func TestVersionJSONNotesUnknownCommit(t *testing.T) {
	note := commitUnknownNote("v0.1.0")
	if body := versionJSON("v0.1.0", "1da0b1f57715", note); body["commit_note"] != "" {
		t.Fatalf("known commit must not carry a note, got %q", body["commit_note"])
	}
	body := versionJSON("v0.1.0", unknownCommit, note)
	if body["commit_note"] != note {
		t.Fatalf("unknown commit note = %q, want explanation", body["commit_note"])
	}
}

func TestSelfUpdateCheckJSONNotesUnknownCommit(t *testing.T) {
	latest := moduleVersion{Version: "v0.1.0", Time: "2026-08-18T06:39:10Z"}
	note := commitUnknownNote("v0.1.0")
	if body := selfUpdateCheckJSON("v0.1.0", "1da0b1f57715", note, latest); body["current_commit_note"] != "" {
		t.Fatalf("known commit must not carry a note, got %q", body["current_commit_note"])
	}
	body := selfUpdateCheckJSON("v0.1.0", unknownCommit, note, latest)
	if body["current_commit_note"] != note {
		t.Fatalf("unknown commit note = %q, want explanation", body["current_commit_note"])
	}
	if body["latest_version"] != "v0.1.0" || body["latest_time"] != "2026-08-18T06:39:10Z" {
		t.Fatalf("latest fields lost: %v", body)
	}
}
