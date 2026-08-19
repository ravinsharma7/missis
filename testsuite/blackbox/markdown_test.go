package blackbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownImportNew(t *testing.T) {
	t.Parallel()
	// covers PH3-MD-001 PH3-MD-002
	store := filepath.Join(t.TempDir(), "missis.db")
	file := filepath.Join(t.TempDir(), "issue.md")
	content := "# Imported issue\n\n## Problem\n\nThe problem body.\n\n## Evidence\n\nEvidence body.\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissis(t, store, "new", "--json", "--from", file)
	if result.code != 0 {
		t.Fatalf("new --from failed: %d %s", result.code, result.stderr)
	}
	created := mustJSON(t, result)
	ref := created["ref"].(string)
	shown := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	parts := shown["parts"].(map[string]any)
	if _, ok := parts["problem"]; !ok {
		t.Fatalf("imported problem part missing: %v", parts)
	}
}

func TestMarkdownImportSet(t *testing.T) {
	t.Parallel()
	// covers PH3-MD-003 PH3-MD-004
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "import target")
	file := filepath.Join(t.TempDir(), "issue.md")
	if err := os.WriteFile(file, []byte("# Extra\n\n## Detail\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissis(t, store, "set", "--json", created["ref"].(string), "--from", file)
	if result.code != 0 {
		t.Fatalf("set --from failed: %d %s", result.code, result.stderr)
	}
	shown := mustJSON(t, runMissis(t, store, "show", "--json", created["ref"].(string)))
	parts := shown["parts"].(map[string]any)
	if _, ok := parts["detail"]; !ok {
		t.Fatalf("imported set part missing: %v", parts)
	}
}

func TestMarkdownExport(t *testing.T) {
	t.Parallel()
	// covers PH3-EXPORT-001
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "Export")
	second := newTicket(t, store, "Target")
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/problem", "problem body", "--kind", "text"); result.code != 0 {
		t.Fatalf("set problem: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/links", "--add", "blocked-by:"+second["ref"].(string)); result.code != 0 {
		t.Fatalf("link: %d %s", result.code, result.stderr)
	}
	result := runMissis(t, store, "show", first["ref"].(string), "--format", "markdown")
	if result.code != 0 {
		t.Fatalf("markdown export failed: %d %s", result.code, result.stderr)
	}
	if !strings.Contains(result.stdout, "# Export") || !strings.Contains(result.stdout, "## problem") || !strings.Contains(result.stdout, "## Links") {
		t.Fatalf("unexpected markdown output:\n%s", result.stdout)
	}
}

func TestMarkdownReimportIdentity(t *testing.T) {
	t.Parallel()
	// covers PH3-REIMPORT-001 PH3-REIMPORT-002
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "reimport")
	file := filepath.Join(t.TempDir(), "issue.md")
	if err := os.WriteFile(file, []byte("# Top\n\n## Problem\n\nFirst body.\n\n## Evidence\n\nEvidence body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := runMissis(t, store, "set", "--json", created["ref"].(string), "--from", file); result.code != 0 {
		t.Fatalf("first import: %d %s", result.code, result.stderr)
	}
	before := mustJSON(t, runMissis(t, store, "show", "--json", created["ref"].(string)))
	beforeParts := before["parts"].(map[string]any)

	if result := runMissis(t, store, "set", "--json", created["ref"].(string), "--from", file); result.code != 0 {
		t.Fatalf("unchanged reimport: %d %s", result.code, result.stderr)
	}
	afterUnchanged := mustJSON(t, runMissis(t, store, "show", "--json", created["ref"].(string)))
	if len(afterUnchanged["parts"].(map[string]any)) != len(beforeParts) {
		t.Fatalf("unchanged reimport changed part count")
	}

	if err := os.WriteFile(file, []byte("# Top\n\n## Problem\n\nUpdated body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := runMissis(t, store, "set", "--json", created["ref"].(string), "--from", file); result.code != 4 {
		t.Fatalf("missing part reimport should fail with validation, got %d %s", result.code, result.stdout)
	}
}

func TestMarkdownRoundTrip(t *testing.T) {
	t.Parallel()
	// covers PH3-ROUNDTRIP-001
	storeA := filepath.Join(t.TempDir(), "a.db")
	storeB := filepath.Join(t.TempDir(), "b.db")
	fixture := filepath.Join(t.TempDir(), "fixture.md")
	content := "# Root\n\n## Problem\n\nThe problem body.\n\n## Evidence\n\n### Empty child\n\n### Race test\n\nEvidence body.\n"
	if err := os.WriteFile(fixture, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	createdA := mustJSON(t, runMissis(t, storeA, "new", "--json", "--from", fixture))
	refA := createdA["ref"].(string)
	exported := runMissis(t, storeA, "show", refA, "--format", "markdown")
	if exported.code != 0 {
		t.Fatalf("export failed: %d %s", exported.code, exported.stderr)
	}
	exportFile := filepath.Join(t.TempDir(), "exported.md")
	if err := os.WriteFile(exportFile, []byte(exported.stdout), 0o644); err != nil {
		t.Fatal(err)
	}

	createdB := mustJSON(t, runMissis(t, storeB, "new", "--json", "--from", exportFile))
	refB := createdB["ref"].(string)
	showA := mustJSON(t, runMissis(t, storeA, "show", "--json", refA))
	showB := mustJSON(t, runMissis(t, storeB, "show", "--json", refB))
	partsA := showA["parts"].(map[string]any)
	partsB := showB["parts"].(map[string]any)
	filteredA := filterSystemParts(partsA)
	filteredB := filterSystemParts(partsB)
	if len(filteredA) != len(filteredB) {
		t.Fatalf("round-trip part count mismatch: %d != %d\nA=%v\nB=%v", len(filteredA), len(filteredB), filteredA, filteredB)
	}
	for path, rawA := range filteredA {
		rawB, ok := filteredB[path]
		if !ok {
			t.Fatalf("missing path %s in round-trip export", path)
		}
		valueA := rawA.(map[string]any)["value"]
		valueB := rawB.(map[string]any)["value"]
		if valueA == nil || valueB == nil {
			continue
		}
		if fmt.Sprint(valueA) != fmt.Sprint(valueB) {
			t.Fatalf("value mismatch for %s: %v != %v", path, valueA, valueB)
		}
	}

	beforeCount := len(filteredA)
	if result := runMissis(t, storeA, "set", "--json", refA, "--from", fixture); result.code != 0 {
		t.Fatalf("reimport into store A failed: %d %s", result.code, result.stderr)
	}
	after := mustJSON(t, runMissis(t, storeA, "show", "--json", refA))
	if len(filterSystemParts(after["parts"].(map[string]any))) != beforeCount {
		t.Fatalf("reimport changed part count")
	}
}

func TestMarkdownDuplicateHeadingsImport(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	file := filepath.Join(t.TempDir(), "issue.md")
	content := "# Dup\n\n## Evidence\nA\n\n## Evidence\nB\n\n## Evidence\nC\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	created := mustJSON(t, runMissis(t, store, "new", "--json", "--from", file))
	shown := mustJSON(t, runMissis(t, store, "show", "--json", created["ref"].(string)))
	parts := shown["parts"].(map[string]any)
	for _, want := range []string{"evidence", "evidence-2", "evidence-3"} {
		if _, ok := parts[want]; !ok {
			t.Fatalf("missing part %s: %v", want, parts)
		}
	}
}

func TestMarkdownPreamblePreserved(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	file := filepath.Join(t.TempDir(), "issue.md")
	content := "Intro text before any heading.\n\n# Title\n\n## Problem\n\nBody.\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	created := mustJSON(t, runMissis(t, store, "new", "--json", "--from", file))
	shown := mustJSON(t, runMissis(t, store, "show", "--json", created["ref"].(string)))
	parts := shown["parts"].(map[string]any)
	preamble, ok := parts["preamble"]
	if !ok {
		t.Fatalf("preamble part missing: %v", parts)
	}
	if preamble.(map[string]any)["value"] != "Intro text before any heading." {
		t.Fatalf("unexpected preamble value: %v", preamble)
	}
	if title, ok := parts["title"]; !ok || title.(map[string]any)["value"] != "title" {
		t.Fatalf("title should come from the H1, not the preamble: %v", parts["title"])
	}
}
