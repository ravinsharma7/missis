package blackbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThreeCommandSurfaceAndArtifactAttachment(t *testing.T) {
	t.Parallel()
	// covers N001
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Attachment")
	ref := created["ref"].(string)
	artifact := filepath.Join(t.TempDir(), "diagram.png")
	if err := os.WriteFile(artifact, []byte("png bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	attached := runMissis(t, store, "set", "--json", ref, "--attach", artifact, "--media-type", "image/png")
	if attached.code != 0 {
		t.Fatalf("attach failed: %d %s", attached.code, attached.stderr)
	}
	view := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	parts, ok := view["parts"].(map[string]any)
	if !ok {
		t.Fatalf("parts missing from projection: %v", view)
	}
	if _, ok := parts["diagram"]; !ok {
		t.Fatalf("attached Part missing: %v", parts)
	}

	unknown := runMissis(t, store, "ingest", "--help")
	if unknown.code != 2 || !strings.Contains(unknown.stderr, "unknown command: ingest") {
		t.Fatalf("fourth top-level ingest command still available: code=%d stderr=%q", unknown.code, unknown.stderr)
	}
}

func TestNewShowSetLifecycle(t *testing.T) {
	t.Parallel()
	// covers PH1-CLI-001 PH1-CLI-002 PH1-CLI-003 PH1-CLI-004 PH1-CLI-005 PH1-EVT-001 PH1-EVT-002 PH1-PRJ-001 PH1-PRV-003 PH1-DM-001 PH1-ACC-001 N002 N022 N057
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Lifecycle")
	ref := created["ref"].(string)

	shown := mustJSON(t, runMissis(t, store, "show", "--json"))
	tickets := shown["tickets"].([]any)
	if len(tickets) != 1 {
		t.Fatalf("expected one ticket, got %d", len(tickets))
	}

	set := runMissis(t, store, "set", "--json", ref+"/status", "doing", "--kind", "status")
	if set.code != 0 {
		t.Fatalf("set failed: %d %s", set.code, set.stderr)
	}
	projection := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	if projection["status"] != "doing" {
		t.Fatalf("status = %v", projection["status"])
	}
}

func TestTicketNumbering(t *testing.T) {
	t.Parallel()
	// covers PH1-REF-002 PH1-DM-002 N022 N024
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "First")
	second := newTicket(t, store, "Second")
	if first["ref"] != "#1" {
		t.Fatalf("first ref = %v", first["ref"])
	}
	if second["ref"] != "#2" {
		t.Fatalf("second ref = %v", second["ref"])
	}
	shown := mustJSON(t, runMissis(t, store, "show", "--json", first["ref"].(string)))
	if shown["id"] != first["id"] {
		t.Fatalf("canonical id mismatch: %v vs %v", shown["id"], first["id"])
	}
}

func TestShowHealth(t *testing.T) {
	t.Parallel()
	// covers PH1-EVT-008
	store := filepath.Join(t.TempDir(), "missis.db")
	newTicket(t, store, "health")
	result := runMissis(t, store, "show", "--health", "--json")
	if result.code != 0 {
		t.Fatalf("health failed: %d %s", result.code, result.stderr)
	}
	body := mustJSON(t, result)
	if body["status"] != "ok" {
		t.Fatalf("health status = %v", body["status"])
	}
}

func TestShowVersion(t *testing.T) {
	t.Parallel()
	// covers PH1-CLI-002
	result := runMissis(t, "", "show", "--version", "--json")
	if result.code != 0 {
		t.Fatalf("version failed: %d %s", result.code, result.stderr)
	}
	body := mustJSON(t, result)
	if body["version"] == nil {
		t.Fatalf("version missing from %s", result.stdout)
	}
}

func TestSelfTrackingBootstrapIsolated(t *testing.T) {
	t.Parallel()
	// covers PH7-SEARCH-003 PH4-SCOPE-004
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "R2 restore verification")
	second := newTicket(t, store, "Backup vs sync decision")
	if result := runMissis(t, store, "set", "--json", second["ref"].(string)+"/links", "--add", "blocked-by:"+first["ref"].(string)); result.code != 0 {
		t.Fatalf("link decision blocked by restore: %d %s", result.code, result.stderr)
	}
	search := mustJSON(t, runMissis(t, store, "show", "--json", "--search", "restore"))
	if len(search["tickets"].([]any)) == 0 {
		t.Fatalf("expected self-tracking search result")
	}
	refs := mustJSON(t, runMissis(t, store, "show", "--json", second["ref"].(string), "--references"))
	if len(refs["links"].([]any)) == 0 {
		t.Fatalf("expected self-tracking references")
	}
}

func filterSystemParts(parts map[string]any) map[string]any {
	filtered := make(map[string]any)
	for path, part := range parts {
		if path == "title" || path == "status" {
			continue
		}
		filtered[path] = part
	}
	return filtered
}
