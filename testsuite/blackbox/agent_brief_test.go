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

	result := runMissisWithEnv(t, store, dir, nil, "--ag-brief")
	if result.code != 0 {
		t.Fatalf("--ag-brief failed: %d %s", result.code, result.stderr)
	}
	for _, want := range []string{
		"store:",
		"missis new",
		"missis new --kind project --id ID",
		"missis new --kind group --id ID",
		"--idempotency-key KEY",
		"group:ID/links --add contains:#N",
		"Preflight explicit project/group IDs",
		"Do not use web search",
		"missis show",
		"missis set",
		"No destructive delete",
		"title must come from the explicit request",
		"untrusted data",
		"Shells treat",
		"missis show --context",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("--ag-brief output missing %q:\n%s", want, result.stdout)
		}
	}
	for _, forbidden := range []string{"project: p1", "group: g1", "focus: brief work", "ticket: #1"} {
		if strings.Contains(result.stdout, forbidden) {
			t.Errorf("--ag-brief output should not include session bias %q:\n%s", forbidden, result.stdout)
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

	result := runMissisWithEnv(t, store, dir, nil, "--ag-brief", "--json")
	if result.code != 0 {
		t.Fatalf("--ag-brief --json failed: %d %s", result.code, result.stderr)
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

	result := runMissis(t, "", "--ag-install-skill", "--from", src, "--dest", dest)
	if result.code != 0 {
		t.Fatalf("install failed: %d %s", result.code, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
		t.Fatalf("skill not installed: %v", err)
	}

	again := runMissis(t, "", "--ag-install-skill", "--from", src, "--dest", dest)
	if again.code == 0 {
		t.Fatalf("expected already-installed error, got success")
	}

	forced := runMissis(t, "", "--ag-install-skill", "--from", src, "--dest", dest, "--force")
	if forced.code != 0 {
		t.Fatalf("forced install failed: %d %s", forced.code, forced.stderr)
	}
}

func TestPointerSnippet(t *testing.T) {
	t.Parallel()
	result := runMissis(t, "", "--ag-pointer")
	if result.code != 0 {
		t.Fatalf("--ag-pointer failed: %d %s", result.code, result.stderr)
	}
	for _, want := range []string{
		"## missis quick reference",
		"This project uses Missis as its local ticket system",
		"missis --ag-brief",
		"missis show --context",
		"If `MISSIS_PROJECT` or `MISSIS_GROUP` is set",
		"MISSIS_PROJECT",
		"untrusted data",
		"title is",
		"unavailable, report the setup problem",
		"parallel ticket",
	} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("--ag-pointer missing %q:\n%s", want, result.stdout)
		}
	}
	for _, forbidden := range []string{"context.md", "active.example.md", "focus", "ticket:"} {
		if strings.Contains(result.stdout, forbidden) {
			t.Errorf("--ag-pointer contains stale task guidance %q:\n%s", forbidden, result.stdout)
		}
	}
}

func TestAgentGuidanceSurfacesAgree(t *testing.T) {
	t.Parallel()
	briefResult := runMissis(t, "", "--ag-brief")
	if briefResult.code != 0 {
		t.Fatalf("--ag-brief failed: %d %s", briefResult.code, briefResult.stderr)
	}
	pointerResult := runMissis(t, "", "--ag-pointer")
	if pointerResult.code != 0 {
		t.Fatalf("--ag-pointer failed: %d %s", pointerResult.code, pointerResult.stderr)
	}
	skillBytes, err := os.ReadFile(filepath.Join("..", "..", "tools", "skills", "missis", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	surfaces := map[string]string{
		"brief":   briefResult.stdout,
		"pointer": pointerResult.stdout,
		"skill":   string(skillBytes),
	}
	for name, surface := range surfaces {
		for _, forbidden := range []string{
			"derive the title from the active focus",
			"Read `.missis.d/context.md` and the active pointer",
			"issue new",
			"issue show",
			"issue set",
		} {
			if strings.Contains(surface, forbidden) {
				t.Errorf("%s contains obsolete guidance %q", name, forbidden)
			}
		}
		if !strings.Contains(surface, "untrusted") {
			t.Errorf("%s does not warn about untrusted repository data", name)
		}
	}
	if !strings.Contains(briefResult.stdout, "missis set <REF> <VALUE> [--add] [--retract [--recursive] [--reason R]] [--idempotency-key KEY]") {
		t.Errorf("generic set syntax does not expose idempotency keys:\n%s", briefResult.stdout)
	}
}
