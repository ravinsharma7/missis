package blackbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHasHomeMembershipEndToEnd(t *testing.T) {
	t.Parallel()
	// covers N080
	store := filepath.Join(t.TempDir(), "missis.db")
	if result := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "safedesign", "SafeDesign"); result.code != 0 {
		t.Fatalf("create project: %d %s", result.code, result.stderr)
	}
	created := runMissis(t, store, "new", "--json", "--project", "safedesign", "Homed ticket")
	if created.code != 0 {
		t.Fatalf("create homed ticket: %d %s", created.code, created.stderr)
	}
	ref := mustJSON(t, created)["ref"].(string)

	projectView := mustJSON(t, runMissis(t, store, "show", "--json", "--project", "safedesign"))
	if len(projectView["tickets"].([]any)) != 1 {
		t.Fatalf("expected homed ticket in project view: %v", projectView)
	}
	refs := runMissis(t, store, "show", "--json", ref, "--references")
	if refs.code != 0 || !strings.Contains(refs.stdout, "has-home") {
		t.Fatalf("references missing has-home: %d %s", refs.code, refs.stdout)
	}

	missing := runMissis(t, store, "new", "--json", "--project", "nope", "Orphan")
	if missing.code != 4 {
		t.Fatalf("missing project should fail validation, got %d: %s", missing.code, missing.stderr)
	}
	missingErr := mustJSON(t, missing)
	if !strings.Contains(missingErr["message"].(string), "missis new --kind project --id nope") {
		t.Fatalf("missing project error should include guidance: %s", missingErr["message"])
	}

	if result := runMissis(t, store, "new", "--json", "--kind", "project", "--id", "eng", "Engineering"); result.code != 0 {
		t.Fatalf("create second project: %d %s", result.code, result.stderr)
	}
	second := runMissis(t, store, "set", "--json", ref+"/links", "--add", "has-home:project:eng")
	if second.code != 4 {
		t.Fatalf("second has-home should fail validation, got %d: %s", second.code, second.stderr)
	}
	secondErr := mustJSON(t, second)
	if !strings.Contains(secondErr["message"].(string), "already has a home project") {
		t.Fatalf("second has-home error should name the existing assertion: %s", secondErr["message"])
	}

	retract := runMissis(t, store, "set", "--json", ref+"/links", "--retract", "has-home:project:safedesign", "--reason", "test")
	if retract.code != 0 {
		t.Fatalf("retract has-home: %d %s", retract.code, retract.stderr)
	}
	retractJSON := mustJSON(t, retract)
	if !strings.Contains(retractJSON["warning"].(string), "zero-home") {
		t.Fatalf("expected zero-home warning in result: %s", retractJSON["warning"])
	}
	projectViewAfter := mustJSON(t, runMissis(t, store, "show", "--json", "--project", "safedesign"))
	if len(projectViewAfter["tickets"].([]any)) != 0 {
		t.Fatalf("project view after retraction should be empty: %v", projectViewAfter)
	}
}
