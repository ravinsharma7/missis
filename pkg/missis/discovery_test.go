package missis

import (
	"os"
	"path/filepath"
	"testing"
)

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
}

func TestResolveStorePathRelativeMarker(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".missis"), []byte("db/store.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, project)

	got, err := ResolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, "db", "store.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathAbsoluteMarker(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	storePath := filepath.Join(tmp, "absolute.db")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".missis"), []byte(storePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, project)

	got, err := ResolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != storePath {
		t.Fatalf("got %q, want %q", got, storePath)
	}
}

func TestResolveStorePathDirectoryMarker(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(project, ".missis"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, project)

	got, err := ResolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, ".missis", "missis.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathInvalidMarker(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "multiple lines", content: "one\ntwo\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			project := filepath.Join(tmp, "project")
			if err := os.MkdirAll(project, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, ".missis"), []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			chdirForTest(t, project)

			if _, err := ResolveStorePath(""); err == nil {
				t.Fatalf("expected error for %s marker", tt.name)
			}
		})
	}
}

func TestResolveStorePathEnvAndOverride(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	storePath := filepath.Join(tmp, "env.db")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, project)
	t.Setenv("MISSIS_STORE", storePath)

	got, err := ResolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != storePath {
		t.Fatalf("env got %q, want %q", got, storePath)
	}

	explicit := filepath.Join(tmp, "explicit.db")
	got, err = ResolveStorePath(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if got != explicit {
		t.Fatalf("override got %q, want %q", got, explicit)
	}
}
