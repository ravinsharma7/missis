package blackbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaDeclarationsEndToEnd(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "schema.db")
	ticket := newTicket(t, store, "Schema governed")
	ref := ticket["ref"].(string)

	if result := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "safedesign", "SafeDesign"); result.code != 0 {
		t.Fatalf("new project: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/links", "--add", "has-home:project:safedesign"); result.code != 0 {
		t.Fatalf("link ticket to project: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "project:safedesign/schema/status", "status", "--kind", "text"); result.code != 0 {
		t.Fatalf("declare status: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "project:safedesign/schema/evidence/*", "evidence", "--kind", "text"); result.code == 0 {
		t.Fatal("wildcard declaration must be rejected")
	}

	// Declared key: no --kind needed (declaration supplies the kind).
	if result := runMissis(t, store, "set", "--json", ref+"/status", "doing"); result.code != 0 {
		t.Fatalf("set status under declaration without --kind: %d %s", result.code, result.stderr)
	}
	// Undeclared key: no fallback, --kind required.
	if result := runMissis(t, store, "set", "--json", ref+"/notes", "scratch"); result.code == 0 {
		t.Fatal("undeclared write without --kind must fail")
	}
	if result := runMissis(t, store, "set", "--json", ref+"/notes", "scratch", "--kind", "text"); result.code != 0 {
		t.Fatalf("undeclared write with --kind: %d %s", result.code, result.stderr)
	}

	// Declared kind wins for rendering: project show exposes the declaration.
	show := runMissis(t, store, "show", "project:safedesign")
	if show.code != 0 {
		t.Fatalf("show project: %d %s", show.code, show.stderr)
	}
	if !strings.Contains(show.stdout, "schema/status: status") {
		t.Fatalf("project show missing declaration:\n%s", show.stdout)
	}

	// Composite enforcement.
	if result := runMissis(t, store, "set", "--json", "project:safedesign/schema/deps", "list[ref]", "--kind", "text"); result.code != 0 {
		t.Fatalf("declare deps: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/deps", "scalar", "--kind", "scalar"); result.code == 0 {
		t.Fatal("scalar write against list[ref] declaration must fail")
	}
	if result := runMissis(t, store, "set", "--json", ref+"/deps", "#1", "--kind", "list"); result.code == 0 {
		t.Fatal("whole-list SetValue against declared list[ref] must fail (use --add)")
	}
	if result := runMissis(t, store, "set", "--json", ref+"/deps", "--add", "#1"); result.code != 0 {
		t.Fatalf("ref element add: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/deps", "--add", "not-a-ref"); result.code == 0 {
		t.Fatal("non-ref element add must fail")
	}

	// Link endpoint legality.
	if result := runMissis(t, store, "set", "--json", "project:safedesign/schema/links/supports", "ref[ticket|part]", "--kind", "text"); result.code != 0 {
		t.Fatalf("declare supports legality: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/links", "--add", "supports:artifact:xyz"); result.code == 0 {
		t.Fatal("artifact target must be rejected by supports legality declaration")
	}
	if result := runMissis(t, store, "set", "--json", ref+"/links", "--add", "supports:#1"); result.code != 0 {
		t.Fatalf("ticket target must be allowed: %d %s", result.code, result.stderr)
	}
}
