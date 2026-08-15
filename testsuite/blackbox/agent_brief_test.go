package blackbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentBrief(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".missis.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	active := "store: .missis-store/missis.db\nproject: p1\ngroup: g1\nfocus: brief work\nticket: #1\n"
	if err := os.WriteFile(filepath.Join(dir, ".missis.d", "active.example.md"), []byte(active), 0o644); err != nil {
		t.Fatal(err)
	}

	result := runMissisWithEnv(t, store, dir, nil, "--agent-brief")
	if result.code != 0 {
		t.Fatalf("--agent-brief failed: %d %s", result.code, result.stderr)
	}
	for _, want := range []string{
		"store:",
		"project: p1",
		"group: g1",
		"focus: brief work",
		"ticket: #1",
		"missis new",
		"missis show",
		"missis set",
		"No destructive delete",
		"do not block on a question",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("--agent-brief output missing %q:\n%s", want, result.stdout)
		}
	}
}

func TestAgentBriefJSON(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".missis.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	active := "project: none\ngroup: none\nfocus: json focus\nticket: #3\n"
	if err := os.WriteFile(filepath.Join(dir, ".missis.d", "active.local.md"), []byte(active), 0o644); err != nil {
		t.Fatal(err)
	}

	result := runMissisWithEnv(t, store, dir, nil, "--agent-brief", "--json")
	if result.code != 0 {
		t.Fatalf("--agent-brief --json failed: %d %s", result.code, result.stderr)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, result.stdout)
	}
	if body["focus"] != "json focus" {
		t.Errorf("focus = %v", body["focus"])
	}
	if body["ticket"] != "#3" {
		t.Errorf("ticket = %v", body["ticket"])
	}
	commands, ok := body["commands"].([]any)
	if !ok || len(commands) == 0 {
		t.Errorf("commands missing or empty: %v", body["commands"])
	}
	rules, ok := body["rules"].([]any)
	if !ok || len(rules) == 0 {
		t.Errorf("rules missing or empty: %v", body["rules"])
	}
}
