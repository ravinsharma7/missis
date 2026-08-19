package blackbox

import (
	"path/filepath"
	"testing"
)

func TestTypedLinksLifecycle(t *testing.T) {
	t.Parallel()
	// covers PH2-LINK-001 PH2-LINK-002 PH2-LINK-003
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "first")
	second := newTicket(t, store, "second")

	add := runMissis(t, store, "set", "--json", first["ref"].(string)+"/links", "--add", "blocked-by:"+second["ref"].(string))
	if add.code != 0 {
		t.Fatalf("link add failed: %d %s", add.code, add.stderr)
	}
	refs := mustJSON(t, runMissis(t, store, "show", "--json", first["ref"].(string), "--references"))
	links := refs["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("expected one link, got %d: %v", len(links), links)
	}
	link := links[0].(map[string]any)
	if link["relation"] != "blocked-by" || link["direction"] != "asserted" {
		t.Fatalf("unexpected link: %v", link)
	}

	inverse := mustJSON(t, runMissis(t, store, "show", "--json", second["ref"].(string), "--references"))
	inverseLinks := inverse["links"].([]any)
	if len(inverseLinks) != 1 || inverseLinks[0].(map[string]any)["relation"] != "blocks" {
		t.Fatalf("unexpected inverse links: %v", inverseLinks)
	}

	retract := runMissis(t, store, "set", "--json", first["ref"].(string)+"/links", "--retract", "blocked-by:"+second["ref"].(string))
	if retract.code != 0 {
		t.Fatalf("link retract failed: %d %s", retract.code, retract.stderr)
	}
	after := mustJSON(t, runMissis(t, store, "show", "--json", first["ref"].(string), "--references"))
	if len(after["links"].([]any)) != 0 {
		t.Fatalf("expected no current links after retract: %v", after["links"])
	}
}

func TestTypedLinksRejectMissingTarget(t *testing.T) {
	t.Parallel()
	// covers PH2-LINK-004
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "first")
	bad := runMissis(t, store, "set", "--json", first["ref"].(string)+"/links", "--add", "blocked-by:#9999")
	if bad.code != 3 {
		t.Fatalf("expected missing target failure, got %d %s", bad.code, bad.stdout)
	}
	malformed := runMissis(t, store, "set", "--json", first["ref"].(string)+"/links", "--add", "blocked-by")
	if malformed.code != 2 {
		t.Fatalf("expected malformed link failure, got %d %s", malformed.code, malformed.stdout)
	}
}

func TestLineageTraversal(t *testing.T) {
	t.Parallel()
	// covers PH2-LINEAGE-001 PH2-LINEAGE-002 PH2-LINEAGE-003
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "first")
	second := newTicket(t, store, "second")
	third := newTicket(t, store, "third")

	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/links", "--add", "blocked-by:"+second["ref"].(string)); result.code != 0 {
		t.Fatalf("link first->second: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", second["ref"].(string)+"/links", "--add", "caused-by:"+third["ref"].(string)); result.code != 0 {
		t.Fatalf("link second->third: %d %s", result.code, result.stderr)
	}

	lineage := mustJSON(t, runMissis(t, store, "show", "--json", first["ref"].(string), "--lineage", "--direction", "both", "--depth", "3"))
	edges := lineage["edges"].([]any)
	if len(edges) != 2 {
		t.Fatalf("expected 2 lineage edges, got %d: %v", len(edges), edges)
	}

	shallow := mustJSON(t, runMissis(t, store, "show", "--json", first["ref"].(string), "--lineage", "--depth", "1"))
	if len(shallow["edges"].([]any)) != 1 {
		t.Fatalf("expected one shallow edge: %v", shallow["edges"])
	}

	filtered := mustJSON(t, runMissis(t, store, "show", "--json", first["ref"].(string), "--lineage", "--relations", "blocked-by"))
	if len(filtered["edges"].([]any)) != 1 {
		t.Fatalf("expected one filtered edge: %v", filtered["edges"])
	}
}

func TestPartLevelLinks(t *testing.T) {
	t.Parallel()
	// covers PH2-LINK-005 PH2-LINK-006
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "first")
	second := newTicket(t, store, "second")

	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/problem", "problem", "--kind", "text"); result.code != 0 {
		t.Fatalf("set first problem: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", second["ref"].(string)+"/evidence", "evidence", "--kind", "text"); result.code != 0 {
		t.Fatalf("set second evidence: %d %s", result.code, result.stderr)
	}

	links := []struct {
		ref    string
		target string
	}{
		{first["ref"].(string) + "/links", second["ref"].(string) + "/evidence"},
		{first["ref"].(string) + "/problem/links", second["ref"].(string) + "/evidence"},
		{second["ref"].(string) + "/evidence/links", first["ref"].(string)},
	}
	for _, link := range links {
		result := runMissis(t, store, "set", "--json", link.ref, "--add", "blocked-by:"+link.target)
		if result.code != 0 {
			t.Fatalf("link %s -> %s: %d %s", link.ref, link.target, result.code, result.stderr)
		}
	}

	refs := mustJSON(t, runMissis(t, store, "show", "--json", first["ref"].(string)+"/problem", "--references"))
	if len(refs["links"].([]any)) == 0 {
		t.Fatalf("expected part-level references: %v", refs["links"])
	}
}
