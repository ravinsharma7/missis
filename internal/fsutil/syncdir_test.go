package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirExistingAndMissing(t *testing.T) {
	dir := t.TempDir()
	if err := SyncDir(dir); err != nil {
		t.Fatalf("sync existing directory: %v", err)
	}
	if err := SyncDir(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("sync missing directory error = %v", err)
	}
}
