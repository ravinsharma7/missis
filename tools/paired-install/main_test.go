package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPairedInstallRefSelection(t *testing.T) {
	// covers PH1-SETUP-004 N125
	for _, tt := range []struct {
		explicit, inferred, want string
		wantErr                  bool
	}{
		{"", "v1.2.3", "v1.2.3", false},
		{"v1.2.3", "", "v1.2.3", false},
		{"v1.2.3", "v1.2.3", "v1.2.3", false},
		{"v1.2.4", "v1.2.3", "", true},
	} {
		got, err := selectRef(tt.explicit, tt.inferred)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Fatalf("selectRef(%q, %q) = %q, %v", tt.explicit, tt.inferred, got, err)
		}
	}
}

func TestPairedInstallDoesNotInferDirtyCheckoutRef(t *testing.T) {
	if got := inferredRef(); strings.Contains(got, "+dirty") {
		t.Fatalf("inferredRef() = %q, want no dirty checkout ref", got)
	}
}

func TestPairedInstallBinDirPrecedenceAndPATH(t *testing.T) {
	missisDir := filepath.Join(t.TempDir(), "missis-bin")
	goDir := filepath.Join(t.TempDir(), "go-bin")
	t.Setenv("MISSIS_BIN_DIR", missisDir)
	t.Setenv("GOBIN", goDir)
	t.Setenv("PATH", missisDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := defaultBinDir()
	if err != nil || got != missisDir {
		t.Fatalf("defaultBinDir = %q, %v", got, err)
	}
	if !pathContains(missisDir) || pathContains(goDir) {
		t.Fatalf("PATH visibility mismatch: missis=%v go=%v", pathContains(missisDir), pathContains(goDir))
	}
	t.Setenv("MISSIS_BIN_DIR", "")
	got, err = defaultBinDir()
	if err != nil || got != goDir {
		t.Fatalf("GOBIN fallback = %q, %v", got, err)
	}
}
