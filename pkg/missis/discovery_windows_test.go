//go:build windows

package missis

import (
	"path/filepath"
	"testing"
)

// These tests only run on Windows (see ticket #55). They verify that drive
// letters and UNC paths resolve as explicit store locations.

func TestResolveStoreWindowsDriveLetter(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveStore(`C:\data\missis.db`)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != filepath.Clean(`C:\data\missis.db`) {
		t.Fatalf("path = %q", resolved.Path)
	}
	if resolved.Source != DiscoveryFlag {
		t.Fatalf("source = %q, want flag", resolved.Source)
	}
}

func TestResolveStoreWindowsUNC(t *testing.T) {
	t.Parallel()
	resolved, err := ResolveStore(`\\server\share\missis.db`)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != filepath.Clean(`\\server\share\missis.db`) {
		t.Fatalf("path = %q", resolved.Path)
	}
	if resolved.Source != DiscoveryFlag {
		t.Fatalf("source = %q, want flag", resolved.Source)
	}
}
