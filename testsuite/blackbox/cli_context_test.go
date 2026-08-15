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
	if err := os.MkdirAll(filepath.Join(project, ".missis.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".missis"), []byte("./.missis-store/missis.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(project, ".missis.d", "active.example.md")
	if err := os.WriteFile(active, []byte("project: test-project\ngroup: test-group\nfocus: context-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMissisWithEnv(t, "", project, nil, "show", "--context", "--json")
	if result.code != 0 {
		t.Fatalf("context failed: %d %s", result.code, result.stderr)
	}
	body := mustJSON(t, result)
	if body["project"] != "test-project" || body["group"] != "test-group" {
		t.Fatalf("unexpected context: %v", body)
	}
}

func TestInitHermetic(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	first := runMissisWithEnv(t, "", project, nil, "--init", "--store", ".missis-store/missis.db", "--json")
	if first.code != 0 {
		t.Fatalf("init failed: %d %s", first.code, first.stderr)
	}
	body := mustJSON(t, first)
	if body["status"] != "initialized" {
		t.Fatalf("unexpected init result: %v", body)
	}
	if _, err := os.Stat(filepath.Join(project, ".missis")); err != nil {
		t.Fatalf(".missis marker missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".missis-store", "missis.db")); err != nil {
		t.Fatalf("store missing: %v", err)
	}
	second := runMissisWithEnv(t, "", project, nil, "--init", "--store", ".missis-store/missis.db", "--json")
	if second.code != 0 {
		t.Fatalf("second init failed: %d %s", second.code, second.stderr)
	}
	secondBody := mustJSON(t, second)
	if secondBody["status"] != "already_initialized" {
		t.Fatalf("unexpected second init result: %v", secondBody)
	}
}

func TestTitleEditPreservesHistory(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Old Title")
	ref := created["ref"].(string)

	set := runMissis(t, store, "set", "--json", ref+"/title", "New Title")
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
