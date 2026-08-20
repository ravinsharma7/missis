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
	for _, g := range []string{"g1", "g2"} {
		if result := runMissis(t, store, "new", "--json", "--kind", "group", "--id", g, g); result.code != 0 {
			t.Fatalf("create group %s: %d %s", g, result.code, result.stderr)
		}
	}
	if result := runMissis(t, store, "new", "--json", "--project", "p1", "T1"); result.code != 0 {
		t.Fatalf("create t1: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "new", "--json", "--project", "p2", "T2"); result.code != 0 {
		t.Fatalf("create t2: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "new", "--json", "T3"); result.code != 0 {
		t.Fatalf("create t3: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "group:g1/links", "--add", "contains:project:p1"); result.code != 0 {
		t.Fatalf("link g1: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "group:g2/links", "--add", "contains:project:p2"); result.code != 0 {
		t.Fatalf("link g2: %d %s", result.code, result.stderr)
	}

	projectUnion := runMissis(t, store, "show", "--json", "--project", "p1", "--project", "p2")
	if tickets := mustJSON(t, projectUnion)["tickets"].([]any); len(tickets) != 2 {
		t.Fatalf("repeated project flags should union: %v", tickets)
	}
	groupUnion := runMissis(t, store, "show", "--json", "--group", "g1", "--group", "g2")
	if tickets := mustJSON(t, groupUnion)["tickets"].([]any); len(tickets) != 2 {
		t.Fatalf("repeated group flags should union: %v", tickets)
	}
	combined := runMissis(t, store, "show", "--json", "--project", "p1", "--group", "g2")
	if tickets := mustJSON(t, combined)["tickets"].([]any); len(tickets) != 0 {
		t.Fatalf("project/group filters should intersect: %v", tickets)
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

	envUnion := runMissisWithEnv(t, store, "", []string{"MISSIS_PROJECT=p1,p2"}, "show", "--json")
	if tickets := mustJSON(t, envUnion)["tickets"].([]any); len(tickets) != 2 {
		t.Fatalf("comma-separated project env should union: %v", tickets)
	}
	envGroupUnion := runMissisWithEnv(t, store, "", []string{"MISSIS_GROUP=g1,g2"}, "show", "--json")
	if tickets := mustJSON(t, envGroupUnion)["tickets"].([]any); len(tickets) != 2 {
		t.Fatalf("comma-separated group env should union: %v", tickets)
	}

	unscoped := runMissisWithEnv(t, store, "", []string{"MISSIS_PROJECT=p1"}, "show", "--json", "--unscoped")
	if unscoped.code != 0 {
		t.Fatalf("explicit unscoped should override env context: %d %s", unscoped.code, unscoped.stderr)
	}
	if tickets := mustJSON(t, unscoped)["tickets"].([]any); len(tickets) != 1 {
		t.Fatalf("unscoped filter should return T3 only: %v", tickets)
	}
	conflict := runMissis(t, store, "show", "--json", "--unscoped", "--project", "p1")
	if conflict.code == 0 {
		t.Fatal("--unscoped with --project should fail")
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
