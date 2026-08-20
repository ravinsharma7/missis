package blackbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSetupGuideContract(t *testing.T) {
	guidePath := filepath.Join("..", "..", "docs", "agent-setup.md")
	data, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatalf("read setup guide %s: %v", guidePath, err)
	}
	guide := string(data)
	for _, want := range []string{
		"You are setting up Missis in the current project directory.",
		"https://github.com/ravinsharma7/missis/blob/<ref>/docs/agent-setup.md",
		"https://raw.githubusercontent.com/ravinsharma7/missis/<ref>/docs/agent-setup.md",
		"## Prerequisites",
		"## Requirements",
		"export MISSIS_REF=v0.2.0",
		"go install \"github.com/ravinsharma7/missis/cmd/missis@$MISSIS_REF\"",
		"go install \"github.com/ravinsharma7/missis/tools/missis-tools@$MISSIS_REF\"",
		"command -v missis-tools",
		"missis-tools --help",
		"go install ./tools/missis-tools",
		"missis --init --json",
		"missis show --health",
		"missis show --context",
		"missis --ag-brief",
		"already initialized",
		"Never use destructive cleanup",
		"Optional agent integrations",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("setup guide missing %q", want)
		}
	}
}

func TestAgentSetupFreshAndRepeat(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	env := []string{"MISSIS_STORE="}

	first := runMissisWithEnv(t, "", project, env, "--init", "--json")
	if first.code != 0 {
		t.Fatalf("first setup failed: %d %s", first.code, first.stderr)
	}
	var firstBody map[string]any
	if err := json.Unmarshal([]byte(first.stdout), &firstBody); err != nil {
		t.Fatalf("first setup JSON: %v\n%s", err, first.stdout)
	}
	if firstBody["status"] != "initialized" {
		t.Fatalf("first setup status = %v", firstBody["status"])
	}

	paths := []string{
		".missis",
		filepath.Join(".missis-store", "missis.db"),
		filepath.Join(".missis.d", "context.md"),
		filepath.Join(".missis.d", "active.example.md"),
	}
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(project, path)); err != nil {
			t.Fatalf("setup artifact %s missing: %v", path, err)
		}
	}

	for _, args := range [][]string{
		{"show", "--health"},
		{"show", "--context"},
		{"--ag-brief"},
	} {
		result := runMissisWithEnv(t, "", project, env, args...)
		if result.code != 0 {
			t.Fatalf("setup verification %v failed: %d %s", args, result.code, result.stderr)
		}
	}

	contextPath := filepath.Join(project, ".missis.d", "context.md")
	activePath := filepath.Join(project, ".missis.d", "active.example.md")
	if err := os.WriteFile(contextPath, []byte("project-owned context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activePath, []byte("project-owned active pointer\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second := runMissisWithEnv(t, "", project, env, "--init", "--json")
	if second.code != 0 {
		t.Fatalf("repeat setup failed: %d %s", second.code, second.stderr)
	}
	var secondBody map[string]any
	if err := json.Unmarshal([]byte(second.stdout), &secondBody); err != nil {
		t.Fatalf("repeat setup JSON: %v\n%s", err, second.stdout)
	}
	if secondBody["status"] != "already_initialized" {
		t.Fatalf("repeat setup status = %v", secondBody["status"])
	}

	for path, want := range map[string]string{
		contextPath: "project-owned context\n",
		activePath:  "project-owned active pointer\n",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("repeat setup changed %s: got %q", path, got)
		}
	}

	health := runMissisWithEnv(t, "", project, env, "show", "--health")
	if health.code != 0 {
		t.Fatalf("health after repeat setup failed: %d %s", health.code, health.stderr)
	}
}
