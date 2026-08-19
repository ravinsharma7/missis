package blackbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestScopeEntityListingAndGroupDirectContains(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	for _, e := range []struct{ kind, id, title string }{
		{"project", "safedesign", "SafeDesign"},
		{"project", "eng", "Engineering"},
		{"group", "security", "Security"},
	} {
		if result := runMissis(t, store, "new", "--json", "--kind", e.kind, "--id", e.id, e.title); result.code != 0 {
			t.Fatalf("create %s: %d %s", e.kind, result.code, result.stderr)
		}
	}

	projects := runMissis(t, store, "show", "--json", "--kind", "project")
	if projects.code != 0 {
		t.Fatalf("show --kind project: %d %s", projects.code, projects.stderr)
	}
	if entities := mustJSON(t, projects)["entities"].([]any); len(entities) != 2 {
		t.Fatalf("expected 2 projects: %v", entities)
	}
	groups := runMissis(t, store, "show", "--json", "--kind", "group")
	if entities := mustJSON(t, groups)["entities"].([]any); len(entities) != 1 {
		t.Fatalf("expected 1 group: %s", groups.stdout)
	}
	searched := runMissis(t, store, "show", "--json", "--kind", "project", "--search", "Safe")
	if entities := mustJSON(t, searched)["entities"].([]any); len(entities) != 1 {
		t.Fatalf("expected 1 project after search: %s", searched.stdout)
	}
	if invalid := runMissis(t, store, "show", "--kind", "bogus"); invalid.code != 2 {
		t.Fatalf("invalid kind should exit 2, got %d", invalid.code)
	}

	detail := runMissis(t, store, "show", "--json", "project:safedesign")
	if detail.code != 0 || !strings.Contains(detail.stdout, "SafeDesign") {
		t.Fatalf("project detail should show real title: %d %s", detail.code, detail.stdout)
	}
	if result := runMissis(t, store, "set", "--json", "project:safedesign/notes", "hello", "--kind", "text"); result.code != 0 {
		t.Fatalf("set project part: %d %s", result.code, result.stderr)
	}
	history := runMissis(t, store, "show", "--json", "project:safedesign", "--history")
	if history.code != 0 || !strings.Contains(history.stdout, "create-entity") {
		t.Fatalf("scope history should include create-entity: %d %s", history.code, history.stdout)
	}
	partHistory := runMissis(t, store, "show", "--json", "project:safedesign/notes", "--history")
	if partHistory.code != 0 || !strings.Contains(partHistory.stdout, "set-value") {
		t.Fatalf("scope part history should include set-value: %d %s", partHistory.code, partHistory.stdout)
	}
	nope := runMissis(t, store, "set", "--json", "project:safedesign/links", "--add", "contains:project:nope")
	if nope.code != 4 {
		t.Fatalf("link to nonexistent project should fail validation, got %d: %s", nope.code, nope.stderr)
	}
	if !strings.Contains(mustJSON(t, nope)["message"].(string), "does not exist") {
		t.Fatalf("link target error should name the target: %s", mustJSON(t, nope)["message"])
	}

	ticket := newTicket(t, store, "Direct member")
	ref := ticket["ref"].(string)
	if result := runMissis(t, store, "set", "--json", "group:security/links", "--add", "contains:"+ref); result.code != 0 {
		t.Fatalf("link ticket to group: %d %s", result.code, result.stderr)
	}
	groupView := mustJSON(t, runMissis(t, store, "show", "--json", "--group", "security"))
	if tickets := groupView["tickets"].([]any); len(tickets) != 1 {
		t.Fatalf("expected direct member in group view: %v", groupView)
	}
}
