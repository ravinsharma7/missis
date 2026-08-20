package blackbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceSemanticsEndToEnd(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	if result := runMissis(t, store, "new", "--json", "Scoped"); result.code != 0 {
		t.Fatalf("create ticket: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "a", "A"); result.code != 0 {
		t.Fatalf("create project: %d %s", result.code, result.stderr)
	}

	first := runMissis(t, store, "set", "--json", "--actor", "human/local", "project:a/links", "--add", "contains:#1")
	if first.code != 0 {
		t.Fatalf("first assert: %d %s", first.code, first.stderr)
	}
	firstAlias := mustJSON(t, first)["event"].(string)
	guarded := runMissis(t, store, "set", "--json", "--actor", "plugin/guarded", "project:a/links", "--add", "contains:#1")
	if guarded.code != 0 || mustJSON(t, guarded)["operation"] != "noop" {
		t.Fatalf("default CLI duplicate guard: %d %s", guarded.code, guarded.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "--actor", "plugin/x", "project:a/links", "--add", "contains:#1", "--allow-duplicate"); result.code != 0 {
		t.Fatalf("second assert: %d %s", result.code, result.stderr)
	}

	refs := runMissis(t, store, "show", "--json", "project:a", "--references")
	if refs.code != 0 {
		t.Fatalf("references: %d %s", refs.code, refs.stderr)
	}
	links := mustJSON(t, refs)["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("expected 1 visible relation: %v", links)
	}
	assertions := links[0].(map[string]any)["assertions"].([]any)
	if len(assertions) != 2 {
		t.Fatalf("expected 2 assertions: %v", assertions)
	}

	// Targeted retraction keeps the relation visible.
	if result := runMissis(t, store, "set", "--json", "--actor", "human/local", "project:a/links", "--retract", "contains:#1", "--assertion", firstAlias); result.code != 0 {
		t.Fatalf("targeted retract: %d %s", result.code, result.stderr)
	}
	refs = runMissis(t, store, "show", "--json", "project:a", "--references")
	links = mustJSON(t, refs)["links"].([]any)
	if len(links) != 1 || len(links[0].(map[string]any)["assertions"].([]any)) != 1 {
		t.Fatalf("relation should stay visible with 1 assertion: %v", links)
	}

	// Plain retract retracts all remaining assertions.
	if result := runMissis(t, store, "set", "--json", "--actor", "human/local", "project:a/links", "--retract", "contains:#1"); result.code != 0 {
		t.Fatalf("plain retract: %d %s", result.code, result.stderr)
	}
	refs = runMissis(t, store, "show", "--json", "project:a", "--references")
	if !strings.Contains(refs.stdout, `"links":[]`) {
		t.Fatalf("relation should be hidden: %s", refs.stdout)
	}
}
