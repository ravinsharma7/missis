package main

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/update"
)

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

func TestSelfUpdateResultCarriesBaseAndDisplayVersions(t *testing.T) {
	release := update.ReleaseManifest{
		Version:             "v0.2.2",
		Commit:              "1234567890abcdef1234567890abcdef12345678",
		StoreFormatRevision: 2,
	}
	result := selfUpdateResult("updated", release)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Status              string `json:"status"`
		LatestVersion       string `json:"latest_version"`
		DisplayVersion      string `json:"display_version"`
		Commit              string `json:"commit"`
		StoreFormatRevision int    `json:"store_format_revision"`
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "updated" || got.LatestVersion != "v0.2.2" || got.DisplayVersion != "v0.2.2+g1234567890ab" || got.Commit != release.Commit || got.StoreFormatRevision != 2 {
		t.Fatalf("self-update result = %#v", got)
	}
}
