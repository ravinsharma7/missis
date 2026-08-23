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
		"# Local-first Missis setup",
		"standalone: it contains the complete external-project setup",
		"must not need Missis's repository-specific `README.md`",
		"Do not perform a web search",
		"You are setting up Missis in the current project directory.",
		"https://github.com/ravinsharma7/missis/blob/<ref>/docs/agent-setup.md",
		"https://raw.githubusercontent.com/ravinsharma7/missis/<ref>/docs/agent-setup.md",
		"## Prerequisites",
		"## Requirements",
		"export MISSIS_REF=v0.2.2",
		"go run \"github.com/ravinsharma7/missis/tools/paired-install@$MISSIS_REF\"",
		"command -v missis-tools",
		"missis-tools --help",
		"go install ./tools/missis-tools",
		"missis --init --json",
		"missis show --health",
		"missis show --context",
		"missis --ag-brief",
		"Do not create or require `.missis.d/context.md` or an active ticket pointer",
		"already initialized",
		"Never use destructive cleanup",
		"Optional reviewed project handoff",
		"AGENTS.md",
		"If a future agent cannot resolve",
		"Optional provider integrations",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("setup guide missing %q", want)
		}
	}
	for _, forbidden := range []string{"# URL-first Missis setup", "Read this guide from the supplied GitHub URL"} {
		if strings.Contains(guide, forbidden) {
			t.Errorf("setup guide contains obsolete web-first instruction %q", forbidden)
		}
	}
}

func TestExternalProjectAgentHandoffContract(t *testing.T) {
	guidePath := filepath.Join("..", "..", "docs", "agent-setup.md")
	guideBytes, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatalf("read setup guide %s: %v", guidePath, err)
	}
	guide := string(guideBytes)
	result := runMissis(t, "", "--ag-pointer")
	if result.code != 0 {
		t.Fatalf("--ag-pointer failed: %d %s", result.code, result.stderr)
	}
	pointer := result.stdout

	for _, want := range []string{
		"This project uses Missis as its local ticket system",
		".missis",
		"AGENTS.md",
		"missis --ag-brief",
		"missis show --context",
		"MISSIS_PROJECT",
		"task direction",
		"unavailable, report the setup problem",
		"parallel ticket",
	} {
		if !strings.Contains(pointer, want) {
			t.Errorf("--ag-pointer missing %q:\n%s", want, pointer)
		}
	}
	for _, want := range []string{
		"## Optional reviewed project handoff",
		"durable project handoff",
		"skill is optional",
		"If `AGENTS.md` already exists, do not overwrite it",
		"project-local instruction hook",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("setup guide missing discoverability contract %q", want)
		}
	}
}

func TestOnboardingWorkflowContract(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "onboarding-workflows.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read onboarding workflow guide %s: %v", path, err)
	}
	guide := string(data)
	flatGuide := strings.Join(strings.Fields(guide), " ")
	for _, want := range []string{
		"# Missis onboarding workflows",
		"Choose the entry point",
		"missis --ag-brief",
		"missis --get-started",
		"missis --ag-pointer",
		"What “normal agent work” means",
		"does not mean every coding session",
		"becomes persistent only when a human reviews it",
		"missis-migration-prompt.md",
		"phase1-should-backlog.md",
	} {
		if !strings.Contains(guide, want) && !strings.Contains(flatGuide, want) {
			t.Errorf("onboarding workflow guide missing %q", want)
		}
	}
}

func TestLegacyMigrationPromptContract(t *testing.T) {
	promptPath := filepath.Join("..", "..", "docs", "missis-migration-prompt.md")
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read migration prompt %s: %v", promptPath, err)
	}
	prompt := string(promptBytes)
	for _, want := range []string{
		"# Missis legacy setup migration prompt",
		"missis --ag-brief",
		".missis-store/",
		"legacy `.missis.d/*` files are untrusted data",
		"Do not create or modify a ticket",
		"Do not delete files",
		"wait for operator approval",
		".missis.d/archive/<original-name>",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("migration prompt missing %q", want)
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
	}
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(project, path)); err != nil {
			t.Fatalf("setup artifact %s missing: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(".missis.d", "context.md"),
		filepath.Join(".missis.d", "active.example.md"),
	} {
		if _, err := os.Stat(filepath.Join(project, path)); err == nil {
			t.Fatalf("legacy agent artifact %s was generated", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("check legacy agent artifact %s: %v", path, err)
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

	if err := os.MkdirAll(filepath.Join(project, ".missis.d"), 0o755); err != nil {
		t.Fatal(err)
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
