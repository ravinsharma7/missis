package blackbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPreserveStoreDump(t *testing.T) {
	base := t.TempDir()
	storePath := filepath.Join(t.TempDir(), "missis.db")
	if err := os.WriteFile(storePath, []byte("db-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath+"-wal", []byte("wal-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "TestPreserveStoreDump/sub"
	dir, err := preserveStoreDump(base, name, storePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"missis.db", "missis.db-wal", "metadata.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["test"] != name {
		t.Fatalf("meta test = %v, want %q", meta["test"], name)
	}
	if meta["goos"] != runtime.GOOS {
		t.Fatalf("meta goos = %v, want %q", meta["goos"], runtime.GOOS)
	}
	if _, err := os.Stat(filepath.Join(dir, "missis.db-shm")); !os.IsNotExist(err) {
		t.Fatalf("missing sidecar should be skipped, stat err = %v", err)
	}
}
