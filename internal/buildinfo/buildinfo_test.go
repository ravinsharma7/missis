package buildinfo

import (
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/store"
)

func TestDisplayIncludesCommitAndStoreFormat(t *testing.T) {
	info := Info{Version: "v0.2.2", Commit: "1234567890abcdef", StoreFormatRevision: store.CurrentStoreFormatRevision}
	if got := display(info); got != "v0.2.2+g1234567890ab" {
		t.Fatalf("display = %q", got)
	}
	if info.StoreFormatRevision != 2 {
		t.Fatalf("store format = %d", info.StoreFormatRevision)
	}
	info.Dirty = true
	if got := display(info); !strings.HasSuffix(got, "-dirty") {
		t.Fatalf("dirty display = %q", got)
	}
}

func TestReleaseDisplayKeepsBaseVersionSortable(t *testing.T) {
	if got := ReleaseDisplay("v0.2.2", "1234567890abcdef"); got != "v0.2.2+g1234567890ab" {
		t.Fatalf("release display = %q", got)
	}
	if !IsStable(Info{Version: "v0.2.2", DisplayVersion: "v0.2.2+g1234567890ab", Commit: "1234567890abcdef"}) {
		t.Fatal("display build metadata changed stable base-version classification")
	}
}

func TestStableBuildClassification(t *testing.T) {
	if !IsStable(Info{Version: "v0.2.2"}) {
		t.Fatal("tagged build should be stable")
	}
	if IsStable(Info{Version: "dev", Commit: "abc"}) || IsStable(Info{Version: "v0.2.2", Dirty: true}) {
		t.Fatal("development or dirty build classified as stable")
	}
	if IsStable(Info{Version: "vx"}) || IsStable(Info{Version: "v0.2.2-rc.1"}) {
		t.Fatal("invalid or prerelease version classified as stable")
	}
}
