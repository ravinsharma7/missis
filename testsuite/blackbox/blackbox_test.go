package blackbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var missisBin string

func TestMain(m *testing.M) {
	if env := os.Getenv("MISSIS_BIN"); env != "" {
		missisBin = env
		os.Exit(m.Run())
	}

	tmp, err := os.MkdirTemp("", "missis-bin-*")
	if err != nil {
		panic(err)
	}
	missisBin = filepath.Join(tmp, "missis")
	build := exec.Command("go", "build", "-o", missisBin, "github.com/ravinsharma7/missis/cmd/missis")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(tmp)
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

type cmdResult struct {
	stdout string
	stderr string
	code   int
}

func runMissis(t *testing.T, store string, args ...string) cmdResult {
	t.Helper()
	cmd := exec.Command(missisBin, args...)
	cmd.Env = append(os.Environ(), "MISSIS_STORE="+store)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return cmdResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func newTicket(t *testing.T, store, title string) map[string]any {
	t.Helper()
	result := runMissis(t, store, "new", "--json", title)
	if result.code != 0 {
		t.Fatalf("new failed: %d %s", result.code, result.stderr)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &body); err != nil {
		t.Fatalf("new json: %v\n%s", err, result.stdout)
	}
	return body
}

func mustJSON(t *testing.T, result cmdResult) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, result.stdout)
	}
	return body
}

func TestNewShowSetLifecycle(t *testing.T) {
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Lifecycle")
	ref := created["ref"].(string)

	shown := mustJSON(t, runMissis(t, store, "show", "--json"))
	tickets := shown["tickets"].([]any)
	if len(tickets) != 1 {
		t.Fatalf("expected one ticket, got %d", len(tickets))
	}

	set := runMissis(t, store, "set", "--json", ref+"/status", "doing")
	if set.code != 0 {
		t.Fatalf("set failed: %d %s", set.code, set.stderr)
	}
	projection := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	if projection["status"] != "doing" {
		t.Fatalf("status = %v", projection["status"])
	}
}

func TestNestedPartRenameMoveRetractHistory(t *testing.T) {
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

func TestOptimisticConflict(t *testing.T) {
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
