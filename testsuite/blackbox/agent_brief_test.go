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
		"missis new",
		"missis show",
		"missis set",
		"No destructive delete",
		"do not block on a question",
		"missis show --context",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("--agent-brief output missing %q:\n%s", want, result.stdout)
		}
	}
	for _, forbidden := range []string{"project: p1", "group: g1", "focus: brief work", "ticket: #1"} {
		if strings.Contains(result.stdout, forbidden) {
			t.Errorf("--agent-brief output should not include session bias %q:\n%s", forbidden, result.stdout)
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
	if _, ok := body["focus"]; ok {
		t.Errorf("JSON should not include focus: %v", body["focus"])
	}
	if _, ok := body["ticket"]; ok {
		t.Errorf("JSON should not include ticket: %v", body["ticket"])
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

func TestInstallSkill(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: missis\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()

	result := runMissis(t, "", "--install-skill", "--from", src, "--dest", dest)
	if result.code != 0 {
		t.Fatalf("install failed: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
		t.Fatalf("skill not installed: %v", err)
	}

	again := runMissis(t, "", "--install-skill", "--from", src, "--dest", dest)
	if again.code == 0 {
		t.Fatalf("expected already-installed error, got success")
	}

	forced := runMissis(t, "", "--install-skill", "--from", src, "--dest", dest, "--force")
	if forced.code != 0 {
		t.Fatalf("forced install failed: %d %s", forced.code, forced.stderr)
	}
}

func TestPointerSnippet(t *testing.T) {
	t.Parallel()
	result := runMissis(t, "", "--pointer")
	if result.code != 0 {
		t.Fatalf("--pointer failed: %d %s", result.code, result.stderr)
	}
	for _, want := range []string{"## missis quick reference", "missis --agent-brief", "missis show --context"} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("--pointer missing %q:\n%s", want, result.stdout)
		}
	}
}
