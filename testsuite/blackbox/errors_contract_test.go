package blackbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalVersionAndHelp(t *testing.T) {
	t.Parallel()
	// covers N002
	ver := runMissis(t, "", "--version")
	if ver.code != 0 {
		t.Fatalf("global --version exit = %d stdout=%s stderr=%s", ver.code, ver.stdout, ver.stderr)
	}
	if !strings.Contains(ver.stdout, "missis version=") {
		t.Fatalf("unexpected version output: %q", ver.stdout)
	}

	verJSON := runMissis(t, "", "--version", "--json")
	if verJSON.code != 0 {
		t.Fatalf("global --version --json exit = %d stdout=%s stderr=%s", verJSON.code, verJSON.stdout, verJSON.stderr)
	}
	body := mustJSON(t, verJSON)
	if body["version"] == nil {
		t.Fatalf("version missing from %s", verJSON.stdout)
	}

	help := runMissis(t, "", "--help")
	if help.code != 0 {
		t.Fatalf("global --help exit = %d stdout=%s stderr=%s", help.code, help.stdout, help.stderr)
	}
	if !strings.Contains(help.stdout, "usage:") {
		t.Fatalf("unexpected help output: %q", help.stdout)
	}
}

func TestStructuredJSONErrorShape(t *testing.T) {
	t.Parallel()
	// covers PH1-CLI-006 N113
	store := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, store, "ErrorShape")
	ref := created["ref"].(string)

	result := runMissis(t, store, "set", "--json", ref+"/status", "blocked")
	if result.code != 4 {
		t.Fatalf("expected validation exit 4, got %d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	body := mustJSON(t, result)
	if body["error"] != "validation_failed" {
		t.Fatalf("error code = %v, want validation_failed", body["error"])
	}
	if body["target"] != ref+"/status" {
		t.Fatalf("target = %v, want %s", body["target"], ref+"/status")
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "reason") {
		t.Fatalf("message = %v, want reason mention", body["message"])
	}
	if body["ontology"] != nil {
		t.Fatalf("ontology = %v, want null", body["ontology"])
	}
	obligations, ok := body["missing_obligations"].([]any)
	if !ok || len(obligations) != 0 {
		t.Fatalf("missing_obligations = %v, want empty list", body["missing_obligations"])
	}
}

func TestStorageFailureExitCode(t *testing.T) {
	t.Parallel()
	// covers PH1-CLI-006
	store := filepath.Join(t.TempDir(), "not-a-db")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}

	result := runMissis(t, store, "show", "--json")
	if result.code != 8 {
		t.Fatalf("expected storage exit 8, got %d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
	body := mustJSON(t, result)
	if body["error"] != "storage_failure" {
		t.Fatalf("error code = %v, want storage_failure", body["error"])
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Fatalf("message empty: %v", body)
	}
	if body["ontology"] != nil {
		t.Fatalf("ontology = %v, want null", body["ontology"])
	}
	if _, ok := body["missing_obligations"].([]any); !ok {
		t.Fatalf("missing_obligations missing: %v", body)
	}
}
