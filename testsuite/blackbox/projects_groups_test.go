package blackbox

import (
	"path/filepath"
	"testing"
)

func TestProjectsAndGroups(t *testing.T) {
	t.Parallel()
	// covers PH4-SCOPE-001 PH4-SCOPE-002 PH4-SCOPE-003
	store := filepath.Join(t.TempDir(), "missis.db")
	if result := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "proj", "Project"); result.code != 0 {
		t.Fatalf("create project: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "new", "--json", "--kind", "group", "--id", "eng", "Engineering"); result.code != 0 {
		t.Fatalf("create group: %d %s", result.code, result.stderr)
	}
	ticket := newTicket(t, store, "Scoped ticket")
	if result := runMissis(t, store, "set", "--json", "project:proj/links", "--add", "contains:"+ticket["ref"].(string)); result.code != 0 {
		t.Fatalf("project contains ticket: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "group:eng/links", "--add", "contains:project:proj"); result.code != 0 {
		t.Fatalf("group contains project: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "group:eng/links", "--add", "governs:project:proj"); result.code != 0 {
		t.Fatalf("group governs project: %d %s", result.code, result.stderr)
	}

	projectView := mustJSON(t, runMissis(t, store, "show", "--json", "--project", "proj"))
	if len(projectView["tickets"].([]any)) == 0 {
		t.Fatalf("expected project tickets: %v", projectView)
	}
	groupView := mustJSON(t, runMissis(t, store, "show", "--json", "--group", "eng"))
	if len(groupView["tickets"].([]any)) == 0 {
		t.Fatalf("expected group tickets: %v", groupView)
	}

	duplicate := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "proj", "Duplicate")
	if duplicate.code != 4 {
		t.Fatalf("expected duplicate project failure, got %d", duplicate.code)
	}
	invalidKind := runMissis(t, store, "new", "--json", "--kind", "other", "--id", "x", "Bad")
	if invalidKind.code != 2 {
		t.Fatalf("expected invalid kind failure, got %d", invalidKind.code)
	}
	invalidID := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "", "Bad")
	if invalidID.code != 2 {
		t.Fatalf("expected invalid id failure, got %d", invalidID.code)
	}
}
