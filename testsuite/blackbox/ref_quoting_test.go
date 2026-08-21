package blackbox

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: shells treat "#N" as a comment, so the agent-facing surface must
// support forms that survive unquoted invocation. These lock in the behavior
// the agent brief and skill now document.
func TestShowBareRef(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Bare ref")
	number := strings.TrimPrefix(created["ref"].(string), "#")

	shown := mustJSON(t, runMissis(t, store, "show", "--json", number))
	if shown["id"] != created["id"] {
		t.Fatalf("bare ref %s resolved to %v, want %v", number, shown["id"], created["id"])
	}
}

func TestShowTrailingFlagsAfterRef(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Trailing flags")
	ref := created["ref"].(string)

	jsonResult := runMissis(t, store, "show", ref, "--json")
	if jsonResult.code != 0 {
		t.Fatalf("show %s --json failed: %d %s", ref, jsonResult.code, jsonResult.stderr)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(jsonResult.stdout), &body); err != nil {
		t.Fatalf("show --json after ref is not JSON: %v\n%s", err, jsonResult.stdout)
	}
	if body["id"] != created["id"] {
		t.Fatalf("trailing --json resolved to %v, want %v", body["id"], created["id"])
	}

	markdownResult := runMissis(t, store, "show", ref, "--format", "markdown")
	if markdownResult.code != 0 {
		t.Fatalf("show %s --format markdown failed: %d %s", ref, markdownResult.code, markdownResult.stderr)
	}
	if !strings.Contains(markdownResult.stdout, "# Trailing flags") {
		t.Fatalf("trailing --format markdown not honored:\n%s", markdownResult.stdout)
	}
	bareMarkdown := runMissis(t, store, "show", strings.TrimPrefix(ref, "#"), "--format", "markdown")
	if bareMarkdown.code != 0 {
		t.Fatalf("show bare %s --format markdown failed: %d %s", ref, bareMarkdown.code, bareMarkdown.stderr)
	}
	if !strings.Contains(bareMarkdown.stdout, "# Trailing flags") {
		t.Fatalf("bare --format markdown not honored:\n%s", bareMarkdown.stdout)
	}
}

func TestShowUnknownFlagsRejected(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "Unknown flags")
	ref := created["ref"].(string)

	result := runMissis(t, store, "show", ref, "--markdown")
	if result.code != 2 {
		t.Fatalf("unknown --markdown expected exit 2, got %d stderr=%s", result.code, result.stderr)
	}
	if !strings.Contains(result.stderr, "flag provided but not defined: -markdown") {
		t.Fatalf("expected unknown-flag error, got: %s", result.stderr)
	}
}
