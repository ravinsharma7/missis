package blackbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportMarkdownWithHomeEndToEnd(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	if result := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "safedesign", "SafeDesign"); result.code != 0 {
		t.Fatalf("create project: %d %s", result.code, result.stderr)
	}
	md := filepath.Join(t.TempDir(), "bug.md")
	if err := os.WriteFile(md, []byte("## body\n\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	imported := runMissis(t, store, "new", "--json", "--from", md, "--project", "safedesign")
	if imported.code != 0 {
		t.Fatalf("import: %d %s", imported.code, imported.stderr)
	}
	view := mustJSON(t, runMissis(t, store, "show", "--json", "--project", "safedesign"))
	if tickets := view["tickets"].([]any); len(tickets) != 1 {
		t.Fatalf("expected imported ticket in project view: %v", view)
	}
	missing := runMissis(t, store, "new", "--from", md, "--project", "nope")
	if missing.code != 4 {
		t.Fatalf("missing project should exit 4, got %d: %s", missing.code, missing.stderr)
	}
	if !strings.Contains(missing.stderr, "missis new --kind project --id nope") {
		t.Fatalf("missing project should include guidance: %s", missing.stderr)
	}
}
