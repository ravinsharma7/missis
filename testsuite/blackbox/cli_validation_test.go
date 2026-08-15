package blackbox

import "testing"

func TestNewRequiresTitle(t *testing.T) {
	t.Parallel()
	// covers PH1-CLI-004
	result := runMissis(t, "", "new", "--json")
	if result.code != 2 {
		t.Fatalf("expected new without title to exit 2, got %d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
}

func TestSetRequiresReference(t *testing.T) {
	t.Parallel()
	// covers PH1-CLI-004
	result := runMissis(t, "", "set", "--json")
	if result.code != 2 {
		t.Fatalf("expected set without reference to exit 2, got %d stdout=%s stderr=%s", result.code, result.stdout, result.stderr)
	}
}
