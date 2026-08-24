package blackbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShowContext(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".missis"), []byte("./.missis-store/missis.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", project, []string{"MISSIS_PROJECT=test-project", "MISSIS_GROUP=test-group"}, "show", "--context", "--json")
	if result.code != 0 {
		t.Fatalf("context failed: %d %s", result.code, result.stderr)
	}
	body := mustJSON(t, result)
	if body["project"] != "test-project" || body["group"] != "test-group" {
		t.Fatalf("unexpected context: %v", body)
	}
	if _, ok := body["focus"]; ok {
		t.Fatalf("context must not expose task focus: %v", body)
	}
}

func TestTitleEditPreservesHistory(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Old Title")
	ref := created["ref"].(string)

	set := runMissis(t, store, "set", "--json", ref+"/title", "New Title", "--kind", "text")
	if set.code != 0 {
		t.Fatalf("title edit failed: %d %s", set.code, set.stderr)
	}
	shown := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	if shown["title"] != "New Title" {
		t.Fatalf("title = %v, want New Title", shown["title"])
	}
	history := mustJSON(t, runMissis(t, store, "show", "--json", ref+"/title", "--history"))
	events, ok := history["events"].([]any)
	if !ok || len(events) < 2 {
		t.Fatalf("expected at least 2 title events, got %v", history)
	}
}

func TestCommandsDoNotOverwriteActiveContext(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(project, ".missis.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".missis"), []byte("./.missis-store/missis.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(project, ".missis.d", "active.local.md")
	original := []byte("project: local-project\ngroup: local-group\nfocus: local-focus\n")
	if err := os.WriteFile(activePath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	actions := [][]string{
		{"new", "--json", "Context ticket"},
		{"show", "--json"},
		{"show", "--context", "--json"},
	}
	for _, args := range actions {
		result := runMissisWithEnv(t, "", project, nil, args...)
		if result.code != 0 {
			t.Fatalf("action %v failed: %d %s", args, result.code, result.stderr)
		}
	}
	after, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("active context changed unexpectedly:\nwant %q\ngot  %q", original, after)
	}
	ctx := mustJSON(t, runMissisWithEnv(t, "", project, nil, "show", "--context", "--json"))
	if ctx["project"] != "none" || ctx["group"] != "none" {
		t.Fatalf("legacy active pointer must not select scope: %v", ctx)
	}
}
