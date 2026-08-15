package blackbox

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOptimisticConflict(t *testing.T) {
	t.Parallel()
	// covers PH1-CON-004 N107
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Conflict")
	ref := created["ref"].(string)

	history := mustJSON(t, runMissis(t, store, "show", ref+"/status", "--history", "--json"))
	oldAlias := history["events"].([]any)[0].(map[string]any)["alias"].(string)

	if result := runMissis(t, store, "set", "--json", ref+"/status", "doing"); result.code != 0 {
		t.Fatalf("set: %d %s", result.code, result.stderr)
	}
	conflict := runMissis(t, store, "set", "--json", ref+"/status", "blocked", "--reason", "x", "--if-current", oldAlias)
	if conflict.code != 5 {
		t.Fatalf("expected conflict code 5, got %d %s", conflict.code, conflict.stdout)
	}
}

func TestIdempotency(t *testing.T) {
	t.Parallel()
	// covers PH1-CON-004 N112
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Idempotency")
	ref := created["ref"].(string)

	first := runMissis(t, store, "set", "--json", ref+"/status", "doing", "--idempotency-key", "k1")
	second := runMissis(t, store, "set", "--json", ref+"/status", "doing", "--idempotency-key", "k1")
	if first.code != 0 || second.code != 0 {
		t.Fatalf("idempotent set failed: %d/%d", first.code, second.code)
	}
	if mustJSON(t, first)["event"] != mustJSON(t, second)["event"] {
		t.Fatalf("idempotent responses differ: %s / %s", first.stdout, second.stdout)
	}
}

func TestBlockedRequiresReason(t *testing.T) {
	t.Parallel()
	// covers PH1-CLI-006 PH1-CLI-007 PH1-CLI-008 N004 N005 N006 N113
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Blocked")
	ref := created["ref"].(string)

	bad := runMissis(t, store, "set", "--json", ref+"/status", "blocked")
	if bad.code != 4 {
		t.Fatalf("expected validation code 4, got %d", bad.code)
	}
	good := runMissis(t, store, "set", "--json", ref+"/status", "blocked", "--reason", "waiting")
	if good.code != 0 {
		t.Fatalf("blocked with reason failed: %d %s", good.code, good.stderr)
	}
}

func TestCycleRejected(t *testing.T) {
	t.Parallel()
	// covers PH1-PART-006 PH1-CON-002 N106
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Cycle")
	ref := created["ref"].(string)

	if result := runMissis(t, store, "set", "--json", ref+"/a/b", "child"); result.code != 0 {
		t.Fatalf("create child: %d %s", result.code, result.stderr)
	}
	cycle := runMissis(t, store, "set", "--json", ref+"/a", "--parent", ref+"/a/b")
	if cycle.code == 0 {
		t.Fatalf("expected cycle rejection")
	}
}

func TestBitemporalProjection(t *testing.T) {
	t.Parallel()
	// covers PH1-PRJ-002 PH1-PRJ-003 N042
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Bitemporal")
	ref := created["ref"].(string)

	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	set := runMissis(t, store, "set", "--json", ref+"/status", "doing", "--effective-at", future)
	if set.code != 0 {
		t.Fatalf("set future: %d %s", set.code, set.stderr)
	}

	nowView := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	if nowView["status"] != "open" {
		t.Fatalf("current status = %v, want open", nowView["status"])
	}

	futureView := mustJSON(t, runMissis(t, store, "show", "--json", ref, "--at", future))
	if futureView["status"] != "doing" {
		t.Fatalf("future status = %v, want doing", futureView["status"])
	}
}
