package blackbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setupEnv() []string {
	return []string{"MISSIS_STORE=", "PATH=" + filepath.Dir(missisBin) + string(os.PathListSeparator) + os.Getenv("PATH")}
}

func setupJSON(t *testing.T, result cmdResult) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &body); err != nil {
		t.Fatalf("setup JSON: %v\nstdout=%s\nstderr=%s", err, result.stdout, result.stderr)
	}
	return body
}

func TestSetupFreshAndRepeat(t *testing.T) {
	// covers PH1-SETUP-001 N122
	project := t.TempDir()
	args := []string{"--setup", "--project", project, "--allow-development", "--json"}
	first := runMissisWithEnv(t, "", project, setupEnv(), args...)
	if first.code != 0 || setupJSON(t, first)["status"] != "ready_development" {
		t.Fatalf("first setup failed: exit=%d stdout=%s stderr=%s", first.code, first.stdout, first.stderr)
	}
	marker := filepath.Join(project, ".missis")
	store := filepath.Join(project, ".missis-store", "missis.db")
	for _, path := range []string{marker, store} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("setup artifact %s: %v", path, err)
		}
	}
	markerBefore, _ := os.ReadFile(marker)
	second := runMissisWithEnv(t, "", project, setupEnv(), args...)
	if second.code != 0 {
		t.Fatalf("repeat setup failed: %d %s", second.code, second.stderr)
	}
	markerAfter, _ := os.ReadFile(marker)
	if string(markerAfter) != string(markerBefore) {
		t.Fatalf("repeat setup changed marker: before=%q after=%q", markerBefore, markerAfter)
	}
}

func TestSetupCheckIsReadOnly(t *testing.T) {
	// covers PH1-SETUP-002 N123
	project := t.TempDir()
	env := setupEnv()
	apply := runMissisWithEnv(t, "", project, env, "--setup", "--project", project, "--allow-development", "--json")
	if apply.code != 0 {
		t.Fatalf("apply setup failed: %d %s", apply.code, apply.stderr)
	}
	marker := filepath.Join(project, ".missis")
	store := filepath.Join(project, ".missis-store", "missis.db")
	markerBefore, _ := os.ReadFile(marker)
	storeBefore, _ := os.ReadFile(store)
	sidecarsBefore := map[string][]byte{}
	for _, suffix := range []string{"-wal", "-journal"} {
		if data, err := os.ReadFile(store + suffix); err == nil {
			sidecarsBefore[suffix] = data
		}
	}
	check := runMissisWithEnv(t, "", project, env, "--setup", "--project", project, "--check", "--allow-development", "--json")
	if check.code != 0 {
		t.Fatalf("check setup failed: %d %s", check.code, check.stderr)
	}
	markerAfter, _ := os.ReadFile(marker)
	storeAfter, _ := os.ReadFile(store)
	if string(markerBefore) != string(markerAfter) || string(storeBefore) != string(storeAfter) {
		t.Fatal("read-only setup check changed marker or database bytes")
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		after, err := os.ReadFile(store + suffix)
		before, existed := sidecarsBefore[suffix]
		if existed && (err != nil || string(after) != string(before)) {
			t.Fatalf("read-only setup changed %s", store+suffix)
		}
		if !existed && err == nil {
			t.Fatalf("read-only setup created %s", store+suffix)
		}
	}
}

func TestSetupRequiresExplicitDevelopment(t *testing.T) {
	// covers PH1-SETUP-003 N124
	project := t.TempDir()
	result := runMissisWithEnv(t, "", project, setupEnv(), "--setup", "--project", project, "--json")
	if result.code != 8 || setupJSON(t, result)["status"] != "failed" {
		t.Fatalf("development setup: exit=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, ".missis")); !os.IsNotExist(err) {
		t.Fatalf("failed setup created marker: %v", err)
	}
}

func TestSetupRejectsUnsafeState(t *testing.T) {
	project := t.TempDir()
	env := append(setupEnv(), "MISSIS_STORE="+filepath.Join(t.TempDir(), "outside.db"))
	result := runMissisWithEnv(t, "", project, env, "--setup", "--project", project, "--allow-development", "--json")
	if result.code != 2 || !strings.Contains(result.stdout, "unset MISSIS_STORE") {
		t.Fatalf("MISSIS_STORE conflict: exit=%d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
}

func TestSetupPathAndStateMatrix(t *testing.T) {
	t.Run("path with spaces and custom marker", func(t *testing.T) {
		project := filepath.Join(t.TempDir(), "project with spaces")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(project, ".missis")
		if err := os.WriteFile(marker, []byte("data/custom.db\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		legacy := filepath.Join(project, ".missis.d", "context.md")
		if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(legacy, []byte("preserve me\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := runMissisWithEnv(t, "", project, setupEnv(), "--setup", "--project", project, "--allow-development", "--json")
		if result.code != 0 {
			t.Fatalf("custom setup failed: %d %s", result.code, result.stderr)
		}
		if _, err := os.Stat(filepath.Join(project, "data", "custom.db")); err != nil {
			t.Fatalf("custom store missing: %v", err)
		}
		data, _ := os.ReadFile(legacy)
		if string(data) != "preserve me\n" {
			t.Fatalf("legacy metadata changed: %q", data)
		}
	})

	t.Run("escaping store", func(t *testing.T) {
		project := t.TempDir()
		result := runMissisWithEnv(t, "", project, setupEnv(), "--setup", "--project", project, "--store", "../outside.db", "--allow-development", "--json")
		if result.code != 8 || !strings.Contains(result.stdout, "escapes") {
			t.Fatalf("escaping store accepted: exit=%d stdout=%s", result.code, result.stdout)
		}
	})

	if runtime.GOOS != "windows" {
		t.Run("symlinked store escape", func(t *testing.T) {
			project, outside := t.TempDir(), t.TempDir()
			if err := os.Symlink(outside, filepath.Join(project, ".missis-store")); err != nil {
				t.Fatal(err)
			}
			result := runMissisWithEnv(t, "", project, setupEnv(), "--setup", "--project", project, "--allow-development", "--json")
			if result.code != 8 || !strings.Contains(result.stdout, "outside") {
				t.Fatalf("symlink escape accepted: exit=%d stdout=%s", result.code, result.stdout)
			}
		})
	}

	t.Run("missing explicit scope", func(t *testing.T) {
		project := t.TempDir()
		env := append(setupEnv(), "MISSIS_PROJECT=missing-project")
		result := runMissisWithEnv(t, "", project, env, "--setup", "--project", project, "--allow-development", "--json")
		if result.code != 8 || !strings.Contains(result.stdout, "explicit project scope does not exist") {
			t.Fatalf("missing scope accepted: exit=%d stdout=%s", result.code, result.stdout)
		}
		if _, err := os.Stat(filepath.Join(project, ".missis")); !os.IsNotExist(err) {
			t.Fatalf("scope failure published marker: %v", err)
		}
	})

	t.Run("check missing marker", func(t *testing.T) {
		project := t.TempDir()
		result := runMissisWithEnv(t, "", project, setupEnv(), "--setup", "--project", project, "--check", "--allow-development", "--json")
		if result.code != 8 || setupJSON(t, result)["status"] != "not_ready" {
			t.Fatalf("missing marker check: exit=%d stdout=%s", result.code, result.stdout)
		}
		if _, err := os.Stat(filepath.Join(project, ".missis-store")); !os.IsNotExist(err) {
			t.Fatalf("check created store directory: %v", err)
		}
	})
}

func TestRemovedSetupFlagsAreRejected(t *testing.T) {
	for _, old := range []string{"--init", "--start", "--get-started"} {
		result := runMissis(t, "", old)
		if result.code != 2 || !strings.Contains(result.stderr, "unknown command") {
			t.Fatalf("%s was not rejected: exit=%d stdout=%s stderr=%s", old, result.code, result.stdout, result.stderr)
		}
	}
	help := runMissis(t, "", "--help")
	if help.code != 0 || !strings.Contains(help.stdout, "--setup --project DIR") {
		t.Fatalf("setup help missing: %s %s", help.stdout, help.stderr)
	}
	setupHelp := runMissis(t, "", "--setup", "--help")
	if setupHelp.code != 0 || !strings.Contains(setupHelp.stderr, "allow-development") {
		t.Fatalf("setup flag help failed: exit=%d stdout=%s stderr=%s", setupHelp.code, setupHelp.stdout, setupHelp.stderr)
	}
}

func TestAgentSetupGuideContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "agent-setup.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(data)
	for _, want := range []string{"tools/paired-install@<stable-tag>", "--project . --json", "missis --setup --project . --check --json", "https://raw.githubusercontent.com/ravinsharma7/missis/<stable-tag>/docs/agent-setup.md", "missis --ag-brief", "missis-migration-prompt.md", "storage-compatibility.md"} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q", want)
		}
	}
	for _, forbidden := range []string{"## First project", "## Optional mise", "## Local alpha artifact"} {
		if strings.Contains(guide, forbidden) {
			t.Errorf("guide retains unrelated section %q", forbidden)
		}
	}
}

func TestLegacyMigrationPromptContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "missis-migration-prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(data)
	for _, want := range []string{"missis --ag-brief", ".missis-store/", "Do not create or modify a ticket", "Do not delete files", "wait for operator approval"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("migration prompt missing %q", want)
		}
	}
}
