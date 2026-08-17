package blackbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreFlagWinsOverMarker(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), []byte("marker.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitStore := filepath.Join(tmp, "explicit.db")

	if result := runMissisWithEnv(t, "", projectDir, nil, "new", "--json", "--store", explicitStore, "Explicit"); result.code != 0 {
		t.Fatalf("new explicit: %d %s", result.code, result.stderr)
	}
	if result := runMissisWithEnv(t, "", projectDir, nil, "show", "--json", "--store", explicitStore); result.code != 0 {
		t.Fatalf("show explicit: %d %s", result.code, result.stderr)
	}
	markerView := mustJSON(t, runMissisWithEnv(t, "", projectDir, nil, "show", "--json"))
	if len(markerView["tickets"].([]any)) != 0 {
		t.Fatalf("marker store unexpectedly has tickets: %v", markerView["tickets"])
	}
}

func TestMissisFileRelativeMarker(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), []byte("db/store.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "new", "--json", "Relative")
	if result.code != 0 {
		t.Fatalf("new relative: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "db", "store.db")); err != nil {
		t.Fatalf("relative store not created: %v", err)
	}
}

func TestMissisFileInvalidMultipleLines(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "show", "--json")
	if result.code != 2 {
		t.Fatalf("expected invalid marker exit 2, got %d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
}

func TestMissisFileInvalidEmptyMarker(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "show", "--json")
	if result.code != 2 {
		t.Fatalf("expected invalid empty marker exit 2, got %d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
}

func TestMissisFileAbsoluteMarkerRejected(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	absoluteStore := filepath.Join(tmp, "absolute.db")
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), []byte(absoluteStore+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "new", "--json", "Absolute")
	if result.code == 0 {
		t.Fatalf("absolute marker should be rejected: %s", result.stdout)
	}
	if _, err := os.Stat(absoluteStore); err == nil {
		t.Fatal("absolute store must not be created via a marker")
	}
}

func TestMissisDirectoryMarker(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(projectDir, ".missis"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "new", "--json", "DirMarker")
	if result.code != 0 {
		t.Fatalf("new dir marker: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".missis", "missis.db")); err != nil {
		t.Fatalf("dir marker store not created: %v", err)
	}
}

func TestXDGFallback(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", work, []string{"HOME=" + home}, "new", "--json", "XDG")
	if result.code != 0 {
		t.Fatalf("new xdg: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "missis", "missis.db")); err != nil {
		t.Fatalf("xdg store not created: %v", err)
	}
}

func TestHealthShowsStorePathAndSource(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	newTicket(t, store, "Health")
	health := mustJSON(t, runMissis(t, store, "show", "--health", "--json"))
	if health["discovery_source"] != "flag" {
		t.Fatalf("discovery_source = %v, want flag", health["discovery_source"])
	}
	if health["store_path"] != store {
		t.Fatalf("store_path = %v, want %s", health["store_path"], store)
	}
}

func TestMarkerEscapeRejectedByCLI(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), []byte("../outside.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "show", "--json")
	if result.code == 0 {
		t.Fatalf("escaping marker should be rejected: %s", result.stdout)
	}
}
