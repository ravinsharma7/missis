package blackbox

import (
	"path/filepath"
	"testing"
)

func TestNestedPartRenameMoveRetractHistory(t *testing.T) {
	t.Parallel()
	// covers PH1-PART-001 PH1-PART-002 PH1-PART-003 PH1-PART-004 PH1-PART-005 PH1-PART-006 PH1-PART-007 PH1-PART-010 PH1-PART-011 PH1-PART-012 PH1-REF-001 PH1-REF-003 PH1-REF-004 PH1-EVT-002 PH1-EVT-003 PH1-EVT-004 PH1-EVT-006 PH1-EVT-007 PH1-PRJ-002 PH1-PRJ-003 PH1-PRJ-004 PH1-PRJ-005 PH1-PRV-001 PH1-PRV-002 PH1-PRV-004 N009 N012 N014 N019 N028 N029 N042 N047 N049 N051 N053 N055 N111
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Nested")
	ref := created["ref"].(string)

	set := runMissis(t, store, "set", "--json", ref+"/evidence/race-test", "go test")
	if set.code != 0 {
		t.Fatalf("create nested: %d %s", set.code, set.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/evidence/race-test", "--name", "race-detector"); result.code != 0 {
		t.Fatalf("rename: %d %s", result.code, result.stderr)
	}
	subtree := mustJSON(t, runMissis(t, store, "show", "--json", ref+"/evidence"))
	if _, ok := subtree["parts"].(map[string]any)["evidence/race-detector"]; !ok {
		t.Fatalf("renamed part missing: %v", subtree["parts"])
	}

	history := mustJSON(t, runMissis(t, store, "show", ref+"/evidence", "--history", "--json"))
	events := history["events"].([]any)
	if len(events) == 0 {
		t.Fatalf("expected history events")
	}
}

func TestSupersession(t *testing.T) {
	t.Parallel()
	// covers PH1-EVT-005 N051
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Supersede")
	ref := created["ref"].(string)

	history := mustJSON(t, runMissis(t, store, "show", ref+"/status", "--history", "--json"))
	events := history["events"].([]any)
	statusEvent := events[0].(map[string]any)
	alias := statusEvent["alias"].(string)

	set := runMissis(t, store, "set", "--json", ref+"/status", "doing", "--supersedes", alias)
	if set.code != 0 {
		t.Fatalf("supersede: %d %s", set.code, set.stderr)
	}
	shown := mustJSON(t, runMissis(t, store, "show", "--json", ref+"/status"))
	if shown["status"] != "doing" {
		t.Fatalf("status = %v", shown["status"])
	}
}

func TestParentValueRetractionPreservesChild(t *testing.T) {
	t.Parallel()
	// covers PH1-PART-008 PH1-PART-009 N014
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Parent retraction")
	ref := created["ref"].(string)

	if result := runMissis(t, store, "set", "--json", ref+"/a", "parent"); result.code != 0 {
		t.Fatalf("set parent: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/a/b", "child"); result.code != 0 {
		t.Fatalf("set child: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/a", "--retract", "--reason", "only parent value"); result.code != 0 {
		t.Fatalf("retract parent: %d %s", result.code, result.stderr)
	}

	shown := mustJSON(t, runMissis(t, store, "show", "--json", ref+"/a"))
	parts := shown["parts"].(map[string]any)
	if _, ok := parts["a/b"]; !ok {
		t.Fatalf("child missing after parent value retraction: %v", parts)
	}
}

func TestRecursiveRetractionRemovesSubtree(t *testing.T) {
	t.Parallel()
	// covers PH1-PART-009 N019 N109 N111
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Recursive retraction")
	ref := created["ref"].(string)

	if result := runMissis(t, store, "set", "--json", ref+"/a/b", "child"); result.code != 0 {
		t.Fatalf("set child: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/a", "--retract", "--recursive", "--reason", "remove subtree"); result.code != 0 {
		t.Fatalf("recursive retract: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "show", "--json", ref+"/a"); result.code != 3 {
		t.Fatalf("expected not-found after recursive retract, got %d %s", result.code, result.stdout)
	}
}

func TestStalePathDoesNotRetarget(t *testing.T) {
	t.Parallel()
	// covers PH1-REF-003 N028
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Stale path")
	ref := created["ref"].(string)

	if result := runMissis(t, store, "set", "--json", ref+"/old", "value"); result.code != 0 {
		t.Fatalf("set old: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/old", "--name", "new"); result.code != 0 {
		t.Fatalf("rename: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "show", "--json", ref+"/old"); result.code != 3 {
		t.Fatalf("expected stale path to fail, got %d %s", result.code, result.stdout)
	}
}

func TestAddListAppend(t *testing.T) {
	t.Parallel()
	// covers PH1-PART-013 PH1-CON-003 N015 N110
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "List append")
	ref := created["ref"].(string)

	for _, value := range []string{"one", "two", "one", "has space", "line\nbreak"} {
		result := runMissis(t, store, "set", "--json", ref+"/notes", "--add", value)
		if result.code != 0 {
			t.Fatalf("add %q: %d %s", value, result.code, result.stderr)
		}
	}
	shown := mustJSON(t, runMissis(t, store, "show", "--json", ref+"/notes"))
	parts := shown["parts"].(map[string]any)
	notes := parts["notes"].(map[string]any)
	values, ok := notes["value"].([]any)
	if !ok {
		t.Fatalf("value is not an array: %T %v", notes["value"], notes["value"])
	}
	if len(values) != 5 {
		t.Fatalf("expected 5 values, got %d: %v", len(values), values)
	}
}
