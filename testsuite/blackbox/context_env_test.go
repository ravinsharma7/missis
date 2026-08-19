package blackbox

import (
	"path/filepath"
	"testing"
)

func TestContextEnvDefaultFilter(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	for _, p := range []string{"p1", "p2"} {
		if result := runMissis(t, store, "new", "--json", "--kind", "project", "--id", p, p); result.code != 0 {
			t.Fatalf("create project %s: %d %s", p, result.code, result.stderr)
		}
	}
	if result := runMissis(t, store, "new", "--json", "--project", "p1", "T1"); result.code != 0 {
		t.Fatalf("create t1: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "new", "--json", "--project", "p2", "T2"); result.code != 0 {
		t.Fatalf("create t2: %d %s", result.code, result.stderr)
	}

	envP1 := []string{"MISSIS_PROJECT=p1"}
	filtered := runMissisWithEnv(t, store, "", envP1, "show", "--json")
	if filtered.code != 0 {
		t.Fatalf("show with env context: %d %s", filtered.code, filtered.stderr)
	}
	if tickets := mustJSON(t, filtered)["tickets"].([]any); len(tickets) != 1 {
		t.Fatalf("env context should filter to p1: %v", tickets)
	}

	overridden := runMissisWithEnv(t, store, "", envP1, "show", "--json", "--project", "p2")
	if tickets := mustJSON(t, overridden)["tickets"].([]any); len(tickets) != 1 {
		t.Fatalf("explicit flag must override env context: %v", tickets)
	}

	ctx := runMissisWithEnv(t, store, "", envP1, "show", "--context", "--json")
	if ctx.code != 0 {
		t.Fatalf("show --context: %d %s", ctx.code, ctx.stderr)
	}
	ctxJSON := mustJSON(t, ctx)
	if ctxJSON["project"] != "p1" {
		t.Fatalf("context should reflect env override: %v", ctxJSON)
	}
}
