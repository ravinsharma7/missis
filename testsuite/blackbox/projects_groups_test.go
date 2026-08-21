package blackbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentFacingHermeticScopedOnboarding(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "fresh", "missis.db")
	projectID := "hermetic-app"
	groupID := "hermetic-kb"
	projectRef := "project:" + projectID
	groupRef := "group:" + groupID

	brief := mustJSON(t, runMissis(t, store, "--ag-brief", "--json"))
	rules, ok := brief["rules"].([]any)
	if !ok {
		t.Fatalf("agent brief rules = %T, want []any", brief["rules"])
	}
	for _, want := range []string{
		"Preflight explicit project/group IDs",
		"Scope-shaped ticket tags",
		"stable idempotency key",
		"Do not use web search",
	} {
		found := false
		for _, raw := range rules {
			if text, ok := raw.(string); ok && strings.Contains(text, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("agent brief rules missing %q: %v", want, rules)
		}
	}

	for _, ref := range []string{projectRef, groupRef} {
		preflight := runMissis(t, store, "show", "--json", ref)
		if preflight.code != 3 {
			t.Fatalf("fresh-store preflight for %s = %d, want not-found (3): %s", ref, preflight.code, preflight.stderr)
		}
	}

	badTag := runMissis(t, store, "new", "--json", "--tag", "group:"+groupID, "Wrong scope tag")
	if badTag.code != 2 {
		t.Fatalf("scope-shaped tag should fail closed, got %d", badTag.code)
	}
	if message := mustJSON(t, badTag)["message"].(string); !strings.Contains(message, "scope tag") {
		t.Fatalf("scope-shaped tag error = %q", message)
	}
	missingHome := runMissis(t, store, "new", "--json", "--project", projectID, "--idempotency-key", "missing-home", "Must not be created")
	if missingHome.code != 4 {
		t.Fatalf("ticket before project should fail validation, got %d: %s", missingHome.code, missingHome.stderr)
	}

	projectCreate := runMissis(t, store, "new", "--json", "--kind", "project", "--id", projectID, "--idempotency-key", "hermetic-project", "Hermetic application")
	if projectCreate.code != 0 {
		t.Fatalf("create project: %d %s", projectCreate.code, projectCreate.stderr)
	}
	projectCreateJSON := mustJSON(t, projectCreate)
	projectRetry := runMissis(t, store, "new", "--json", "--kind", "project", "--id", projectID, "--idempotency-key", "hermetic-project", "Hermetic application")
	if projectRetry.code != 0 {
		t.Fatalf("retry project: %d %s", projectRetry.code, projectRetry.stderr)
	}
	if got := mustJSON(t, projectRetry)["ref"]; got != projectCreateJSON["ref"] {
		t.Fatalf("project retry ref = %v, want %v", got, projectCreateJSON["ref"])
	}

	groupCreate := runMissis(t, store, "new", "--json", "--kind", "group", "--id", groupID, "--idempotency-key", "hermetic-group", "Hermetic knowledge base")
	if groupCreate.code != 0 {
		t.Fatalf("create group: %d %s", groupCreate.code, groupCreate.stderr)
	}
	groupCreateJSON := mustJSON(t, groupCreate)
	groupRetry := runMissis(t, store, "new", "--json", "--kind", "group", "--id", groupID, "--idempotency-key", "hermetic-group", "Hermetic knowledge base")
	if groupRetry.code != 0 {
		t.Fatalf("retry group: %d %s", groupRetry.code, groupRetry.stderr)
	}
	if got := mustJSON(t, groupRetry)["ref"]; got != groupCreateJSON["ref"] {
		t.Fatalf("group retry ref = %v, want %v", got, groupCreateJSON["ref"])
	}

	ticketCreate := runMissis(t, store, "new", "--json", "--project", projectID, "--idempotency-key", "hermetic-first-ticket", "First hermetic ticket")
	if ticketCreate.code != 0 {
		t.Fatalf("create first ticket: %d %s", ticketCreate.code, ticketCreate.stderr)
	}
	ticketCreateJSON := mustJSON(t, ticketCreate)
	ticketRef := ticketCreateJSON["ref"].(string)
	ticketRetry := runMissis(t, store, "new", "--json", "--project", projectID, "--idempotency-key", "hermetic-first-ticket", "First hermetic ticket")
	if ticketRetry.code != 0 {
		t.Fatalf("retry first ticket: %d %s", ticketRetry.code, ticketRetry.stderr)
	}
	if got := mustJSON(t, ticketRetry)["ref"]; got != ticketRef {
		t.Fatalf("ticket retry ref = %v, want %s", got, ticketRef)
	}

	groupLinkArgs := []string{"set", "--json", groupRef + "/links", "--add", "contains:" + ticketRef, "--idempotency-key", "hermetic-ticket-group"}
	groupLink := runMissis(t, store, groupLinkArgs...)
	if groupLink.code != 0 {
		t.Fatalf("add group membership: %d %s", groupLink.code, groupLink.stderr)
	}
	groupLinkRetry := runMissis(t, store, groupLinkArgs...)
	if groupLinkRetry.code != 0 {
		t.Fatalf("retry group membership: %d %s", groupLinkRetry.code, groupLinkRetry.stderr)
	}

	projects := mustJSON(t, runMissis(t, store, "show", "--json", "--kind", "project"))["entities"].([]any)
	if len(projects) != 1 || projects[0].(map[string]any)["ref"] != projectRef {
		t.Fatalf("fresh store projects = %v, want exactly [%s]", projects, projectRef)
	}
	groups := mustJSON(t, runMissis(t, store, "show", "--json", "--kind", "group"))["entities"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["ref"] != groupRef {
		t.Fatalf("fresh store groups = %v, want exactly [%s]", groups, groupRef)
	}
	projectView := mustJSON(t, runMissis(t, store, "show", "--json", "--project", projectID))["tickets"].([]any)
	if len(projectView) != 1 || projectView[0].(map[string]any)["ref"] != ticketRef {
		t.Fatalf("project view = %v, want exactly ticket %s", projectView, ticketRef)
	}
	groupView := mustJSON(t, runMissis(t, store, "show", "--json", "--group", groupID))["tickets"].([]any)
	if len(groupView) != 1 || groupView[0].(map[string]any)["ref"] != ticketRef {
		t.Fatalf("group view = %v, want exactly ticket %s", groupView, ticketRef)
	}

	t.Logf("agent-facing hermetic onboarding verified: store=%s project=%s group=%s ticket=%s", store, projectRef, groupRef, ticketRef)
}

func TestProjectsAndGroups(t *testing.T) {
	t.Parallel()
	// covers PH4-SCOPE-001 PH4-SCOPE-002 PH4-SCOPE-003
	store := filepath.Join(t.TempDir(), "missis.db")
	projectCreate := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "proj", "--idempotency-key", "setup-project-proj", "Project")
	if projectCreate.code != 0 {
		t.Fatalf("create project: %d %s", projectCreate.code, projectCreate.stderr)
	}
	projectRetry := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "proj", "--idempotency-key", "setup-project-proj", "Project")
	if projectRetry.code != 0 || mustJSON(t, projectCreate)["ref"] != mustJSON(t, projectRetry)["ref"] {
		t.Fatalf("idempotent project retry failed: %d %s", projectRetry.code, projectRetry.stderr)
	}
	groupCreate := runMissis(t, store, "new", "--json", "--kind", "group", "--id", "eng", "--idempotency-key", "setup-group-eng", "Engineering")
	if groupCreate.code != 0 {
		t.Fatalf("create group: %d %s", groupCreate.code, groupCreate.stderr)
	}
	groupRetry := runMissis(t, store, "new", "--json", "--kind", "group", "--id", "eng", "--idempotency-key", "setup-group-eng", "Engineering")
	if groupRetry.code != 0 || mustJSON(t, groupCreate)["ref"] != mustJSON(t, groupRetry)["ref"] {
		t.Fatalf("idempotent group retry failed: %d %s", groupRetry.code, groupRetry.stderr)
	}
	ticketCreate := runMissis(t, store, "new", "--json", "--project", "proj", "--idempotency-key", "first-ticket", "Scoped ticket")
	if ticketCreate.code != 0 {
		t.Fatalf("create scoped ticket: %d %s", ticketCreate.code, ticketCreate.stderr)
	}
	ticketRetry := runMissis(t, store, "new", "--json", "--project", "proj", "--idempotency-key", "first-ticket", "Scoped ticket")
	if ticketRetry.code != 0 || mustJSON(t, ticketCreate)["ref"] != mustJSON(t, ticketRetry)["ref"] {
		t.Fatalf("idempotent ticket retry failed: %d %s", ticketRetry.code, ticketRetry.stderr)
	}
	ticket := mustJSON(t, ticketCreate)
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
