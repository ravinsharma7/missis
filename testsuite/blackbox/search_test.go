package blackbox

import (
	"path/filepath"
	"testing"
)

func TestSearchAndMetadataFilters(t *testing.T) {
	t.Parallel()
	// covers PH7-SEARCH-001 PH7-SEARCH-002
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "retry race")
	second := newTicket(t, store, "unrelated")
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/problem", "worker retry after shutdown"); result.code != 0 {
		t.Fatalf("set problem: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/status", "doing"); result.code != 0 {
		t.Fatalf("set status: %d %s", result.code, result.stderr)
	}
	search := mustJSON(t, runMissis(t, store, "show", "--json", "--search", "retry"))
	if len(search["tickets"].([]any)) != 1 {
		t.Fatalf("expected one search result: %v", search["tickets"])
	}
	statusView := mustJSON(t, runMissis(t, store, "show", "--json", "--status", "doing"))
	if len(statusView["tickets"].([]any)) != 1 {
		t.Fatalf("expected one doing ticket: %v", statusView["tickets"])
	}
	none := mustJSON(t, runMissis(t, store, "show", "--json", "--search", "missing"))
	if len(none["tickets"].([]any)) != 0 {
		t.Fatalf("expected no search results: %v", none["tickets"])
	}
	_ = second
}
