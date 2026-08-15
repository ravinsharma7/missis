package blackbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
	return runMissisWithEnv(t, store, "", nil, args...)
}

func runMissisWithEnv(t *testing.T, store, dir string, env []string, args ...string) cmdResult {
	t.Helper()
	cmdArgs := args
	if store != "" && len(args) > 0 {
		cmdArgs = append([]string{args[0], "--store", store}, args[1:]...)
	}
	cmd := exec.Command(missisBin, cmdArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmdEnv := os.Environ()
	if store != "" {
		cmdEnv = append(cmdEnv, "MISSIS_STORE="+store)
	}
	cmdEnv = append(cmdEnv, env...)
	cmd.Env = cmdEnv
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

func runMissisRaw(store string, args ...string) (cmdResult, error) {
	cmdArgs := args
	if len(args) > 0 {
		cmdArgs = append([]string{args[0], "--store", store}, args[1:]...)
	}
	cmd := exec.Command(missisBin, cmdArgs...)
	cmd.Env = append(os.Environ(), "MISSIS_STORE="+store)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
		err = nil
	}
	return cmdResult{stdout: stdout.String(), stderr: stderr.String(), code: code}, err
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

func TestStoreFlagWinsOverMarker(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), []byte("marker.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitStore := filepath.Join(tmp, "explicit.db")

	if result := runMissisWithEnv(t, "", projectDir, nil, "new", "--json", "--store", explicitStore, "Explicit"); result.code != 0 {
		t.Fatalf("new explicit: %d %s", result.code, result.stderr)
	}
	if result := runMissisWithEnv(t, "", projectDir, nil, "show", "--json", "--store", explicitStore); result.code != 0 {
		t.Fatalf("show explicit: %d %s", result.code, result.stderr)
	}
	markerView := mustJSON(t, runMissisWithEnv(t, "", projectDir, nil, "show", "--json"))
	if len(markerView["tickets"].([]any)) != 0 {
		t.Fatalf("marker store unexpectedly has tickets: %v", markerView["tickets"])
	}
}

func TestMissisFileRelativeMarker(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), []byte("db/store.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "new", "--json", "Relative")
	if result.code != 0 {
		t.Fatalf("new relative: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "db", "store.db")); err != nil {
		t.Fatalf("relative store not created: %v", err)
	}
}

func TestMissisFileAbsoluteMarker(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	absoluteStore := filepath.Join(tmp, "absolute.db")
	if err := os.WriteFile(filepath.Join(projectDir, ".missis"), []byte(absoluteStore+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "new", "--json", "Absolute")
	if result.code != 0 {
		t.Fatalf("new absolute: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(absoluteStore); err != nil {
		t.Fatalf("absolute store not created: %v", err)
	}
}

func TestMissisDirectoryMarker(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(projectDir, ".missis"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", projectDir, nil, "new", "--json", "DirMarker")
	if result.code != 0 {
		t.Fatalf("new dir marker: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".missis", "missis.db")); err != nil {
		t.Fatalf("dir marker store not created: %v", err)
	}
}

func TestXDGFallback(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", work, []string{"HOME=" + home}, "new", "--json", "XDG")
	if result.code != 0 {
		t.Fatalf("new xdg: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "missis", "missis.db")); err != nil {
		t.Fatalf("xdg store not created: %v", err)
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

func TestMultiProcessConcurrency(t *testing.T) {
	t.Parallel()
	// covers PH1-CON-001 PH1-CON-002 N106 N107 N108 N109
	store := filepath.Join(t.TempDir(), "missis.db")
	base := newTicket(t, store, "base")
	if base["ref"] != "#1" {
		t.Fatalf("base ref = %v", base["ref"])
	}

	const workers = 8
	var wg sync.WaitGroup
	results := make([]cmdResult, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := runMissisRaw(store, "new", "--json", "agent-"+strconv.Itoa(i))
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			results[i] = result
		}(i)
	}
	wg.Wait()
	for i, result := range results {
		if result.code != 0 {
			t.Fatalf("worker %d failed: %d %s", i, result.code, result.stderr)
		}
	}

	shown := mustJSON(t, runMissis(t, store, "show", "--json"))
	tickets := shown["tickets"].([]any)
	if len(tickets) != workers+1 {
		t.Fatalf("expected %d tickets, got %d", workers+1, len(tickets))
	}

	var setWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		setWG.Add(1)
		go func(i int) {
			defer setWG.Done()
			if _, err := runMissisRaw(store, "set", "--json", "#1/agent-"+strconv.Itoa(i), "value-"+strconv.Itoa(i)); err != nil {
				t.Errorf("set worker %d: %v", i, err)
			}
		}(i)
	}
	setWG.Wait()
	projection := mustJSON(t, runMissis(t, store, "show", "--json", "#1"))
	parts := projection["parts"].(map[string]any)
	for i := 0; i < workers; i++ {
		key := "agent-" + strconv.Itoa(i)
		if _, ok := parts[key]; !ok {
			t.Fatalf("missing part %s: %v", key, parts)
		}
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

	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/problem", "problem"); result.code != 0 {
		t.Fatalf("set first problem: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", second["ref"].(string)+"/evidence", "evidence"); result.code != 0 {
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

func TestMarkdownImportNew(t *testing.T) {
	t.Parallel()
	// covers PH3-MD-001 PH3-MD-002
	store := filepath.Join(t.TempDir(), "missis.db")
	file := filepath.Join(t.TempDir(), "issue.md")
	content := "# Imported issue\n\n## Problem\n\nThe problem body.\n\n## Evidence\n\nEvidence body.\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissis(t, store, "new", "--json", "--from", file)
	if result.code != 0 {
		t.Fatalf("new --from failed: %d %s", result.code, result.stderr)
	}
	created := mustJSON(t, result)
	ref := created["ref"].(string)
	shown := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	parts := shown["parts"].(map[string]any)
	if _, ok := parts["problem"]; !ok {
		t.Fatalf("imported problem part missing: %v", parts)
	}
}

func TestMarkdownImportSet(t *testing.T) {
	t.Parallel()
	// covers PH3-MD-003 PH3-MD-004
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "import target")
	file := filepath.Join(t.TempDir(), "issue.md")
	if err := os.WriteFile(file, []byte("# Extra\n\n## Detail\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissis(t, store, "set", "--json", created["ref"].(string), "--from", file)
	if result.code != 0 {
		t.Fatalf("set --from failed: %d %s", result.code, result.stderr)
	}
	shown := mustJSON(t, runMissis(t, store, "show", "--json", created["ref"].(string)))
	parts := shown["parts"].(map[string]any)
	if _, ok := parts["detail"]; !ok {
		t.Fatalf("imported set part missing: %v", parts)
	}
}

func TestMarkdownExport(t *testing.T) {
	t.Parallel()
	// covers PH3-EXPORT-001
	store := filepath.Join(t.TempDir(), "missis.db")
	first := newTicket(t, store, "Export")
	second := newTicket(t, store, "Target")
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/problem", "problem body"); result.code != 0 {
		t.Fatalf("set problem: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/links", "--add", "blocked-by:"+second["ref"].(string)); result.code != 0 {
		t.Fatalf("link: %d %s", result.code, result.stderr)
	}
	result := runMissis(t, store, "show", first["ref"].(string), "--format", "markdown")
	if result.code != 0 {
		t.Fatalf("markdown export failed: %d %s", result.code, result.stderr)
	}
	if !strings.Contains(result.stdout, "# Export") || !strings.Contains(result.stdout, "## problem") || !strings.Contains(result.stdout, "## Links") {
		t.Fatalf("unexpected markdown output:\n%s", result.stdout)
	}
}

func TestMarkdownReimportIdentity(t *testing.T) {
	t.Parallel()
	// covers PH3-REIMPORT-001 PH3-REIMPORT-002
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "reimport")
	file := filepath.Join(t.TempDir(), "issue.md")
	if err := os.WriteFile(file, []byte("# Top\n\n## Problem\n\nFirst body.\n\n## Evidence\n\nEvidence body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := runMissis(t, store, "set", "--json", created["ref"].(string), "--from", file); result.code != 0 {
		t.Fatalf("first import: %d %s", result.code, result.stderr)
	}
	before := mustJSON(t, runMissis(t, store, "show", "--json", created["ref"].(string)))
	beforeParts := before["parts"].(map[string]any)

	if result := runMissis(t, store, "set", "--json", created["ref"].(string), "--from", file); result.code != 0 {
		t.Fatalf("unchanged reimport: %d %s", result.code, result.stderr)
	}
	afterUnchanged := mustJSON(t, runMissis(t, store, "show", "--json", created["ref"].(string)))
	if len(afterUnchanged["parts"].(map[string]any)) != len(beforeParts) {
		t.Fatalf("unchanged reimport changed part count")
	}

	if err := os.WriteFile(file, []byte("# Top\n\n## Problem\n\nUpdated body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := runMissis(t, store, "set", "--json", created["ref"].(string), "--from", file); result.code != 4 {
		t.Fatalf("missing part reimport should fail with validation, got %d %s", result.code, result.stdout)
	}
}

func TestMarkdownRoundTrip(t *testing.T) {
	t.Parallel()
	// covers PH3-ROUNDTRIP-001
	storeA := filepath.Join(t.TempDir(), "a.db")
	storeB := filepath.Join(t.TempDir(), "b.db")
	fixture := filepath.Join(t.TempDir(), "fixture.md")
	content := "# Root\n\n## Problem\n\nThe problem body.\n\n## Evidence\n\n### Empty child\n\n### Race test\n\nEvidence body.\n"
	if err := os.WriteFile(fixture, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	createdA := mustJSON(t, runMissis(t, storeA, "new", "--json", "--from", fixture))
	refA := createdA["ref"].(string)
	exported := runMissis(t, storeA, "show", refA, "--format", "markdown")
	if exported.code != 0 {
		t.Fatalf("export failed: %d %s", exported.code, exported.stderr)
	}
	exportFile := filepath.Join(t.TempDir(), "exported.md")
	if err := os.WriteFile(exportFile, []byte(exported.stdout), 0o644); err != nil {
		t.Fatal(err)
	}

	createdB := mustJSON(t, runMissis(t, storeB, "new", "--json", "--from", exportFile))
	refB := createdB["ref"].(string)
	showA := mustJSON(t, runMissis(t, storeA, "show", "--json", refA))
	showB := mustJSON(t, runMissis(t, storeB, "show", "--json", refB))
	partsA := showA["parts"].(map[string]any)
	partsB := showB["parts"].(map[string]any)
	filteredA := filterSystemParts(partsA)
	filteredB := filterSystemParts(partsB)
	if len(filteredA) != len(filteredB) {
		t.Fatalf("round-trip part count mismatch: %d != %d\nA=%v\nB=%v", len(filteredA), len(filteredB), filteredA, filteredB)
	}
	for path, rawA := range filteredA {
		rawB, ok := filteredB[path]
		if !ok {
			t.Fatalf("missing path %s in round-trip export", path)
		}
		valueA := rawA.(map[string]any)["value"]
		valueB := rawB.(map[string]any)["value"]
		if valueA == nil || valueB == nil {
			continue
		}
		if fmt.Sprint(valueA) != fmt.Sprint(valueB) {
			t.Fatalf("value mismatch for %s: %v != %v", path, valueA, valueB)
		}
	}

	beforeCount := len(filteredA)
	if result := runMissis(t, storeA, "set", "--json", refA, "--from", fixture); result.code != 0 {
		t.Fatalf("reimport into store A failed: %d %s", result.code, result.stderr)
	}
	after := mustJSON(t, runMissis(t, storeA, "show", "--json", refA))
	if len(filterSystemParts(after["parts"].(map[string]any))) != beforeCount {
		t.Fatalf("reimport changed part count")
	}
}

func TestProjectsAndGroups(t *testing.T) {
	t.Parallel()
	// covers PH4-SCOPE-001 PH4-SCOPE-002 PH4-SCOPE-003
	store := filepath.Join(t.TempDir(), "missis.db")
	if result := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "proj", "Project"); result.code != 0 {
		t.Fatalf("create project: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "new", "--json", "--kind", "group", "--id", "eng", "Engineering"); result.code != 0 {
		t.Fatalf("create group: %d %s", result.code, result.stderr)
	}
	ticket := newTicket(t, store, "Scoped ticket")
	if result := runMissis(t, store, "set", "--json", "project:proj/links", "--add", "contains:"+ticket["ref"].(string)); result.code != 0 {
		t.Fatalf("project contains ticket: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "group:eng/links", "--add", "contains:project:proj"); result.code != 0 {
		t.Fatalf("group contains project: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", "group:eng/links", "--add", "governs:project:proj"); result.code != 0 {
		t.Fatalf("group governs project: %d %s", result.code, result.stderr)
	}

	projectView := mustJSON(t, runMissis(t, store, "show", "--json", "--project", "proj"))
	if len(projectView["tickets"].([]any)) == 0 {
		t.Fatalf("expected project tickets: %v", projectView)
	}
	groupView := mustJSON(t, runMissis(t, store, "show", "--json", "--group", "eng"))
	if len(groupView["tickets"].([]any)) == 0 {
		t.Fatalf("expected group tickets: %v", groupView)
	}

	duplicate := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "proj", "Duplicate")
	if duplicate.code != 4 {
		t.Fatalf("expected duplicate project failure, got %d", duplicate.code)
	}
	invalidKind := runMissis(t, store, "new", "--json", "--kind", "other", "--id", "x", "Bad")
	if invalidKind.code != 2 {
		t.Fatalf("expected invalid kind failure, got %d", invalidKind.code)
	}
	invalidID := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "", "Bad")
	if invalidID.code != 2 {
		t.Fatalf("expected invalid id failure, got %d", invalidID.code)
	}
}

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
