package blackbox

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/store"
)

func TestReleaseBinaryIdentity(t *testing.T) {
	// covers PH1-REL-001 PH1-FMT-001
	const (
		version = "v0.2.2"
		commit  = "0123456789abcdef0123456789abcdef01234567"
		display = "v0.2.2+g0123456789ab"
	)
	ldflags := fmt.Sprintf(
		"-X github.com/ravinsharma7/missis/internal/buildinfo.releaseVersion=%s -X github.com/ravinsharma7/missis/internal/buildinfo.releaseCommit=%s",
		version, commit,
	)
	for _, target := range []struct {
		name string
		pkg  string
	}{
		{name: "missis", pkg: "github.com/ravinsharma7/missis/cmd/missis"},
		{name: "missis-tools", pkg: "github.com/ravinsharma7/missis/tools/missis-tools"},
	} {
		t.Run(target.name, func(t *testing.T) {
			name := target.name
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			binary := filepath.Join(t.TempDir(), name)
			build := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", binary, target.pkg)
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build release binary: %v\n%s", err, output)
			}

			output, err := exec.Command(binary, "--version", "--json").Output()
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Version             string `json:"version"`
				DisplayVersion      string `json:"display_version"`
				Commit              string `json:"commit"`
				Dirty               bool   `json:"dirty"`
				StoreFormatRevision int    `json:"store_format_revision"`
			}
			if err := json.Unmarshal(output, &got); err != nil {
				t.Fatal(err)
			}
			if got.Version != version || got.DisplayVersion != display || got.Commit != commit || got.Dirty || got.StoreFormatRevision != store.CurrentStoreFormatRevision {
				t.Fatalf("release identity = %#v", got)
			}

			human, err := exec.Command(binary, "--version").Output()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(human), fmt.Sprintf("version=%s commit=%s store_format=%d", display, commit, store.CurrentStoreFormatRevision)) {
				t.Fatalf("human release identity = %q", human)
			}
		})
	}
}
