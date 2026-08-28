package blackbox

import (
	"strings"
	"testing"
)

func TestStructuredValuesRoundTripThroughCLI(t *testing.T) {
	t.Parallel()
	store := t.TempDir() + "/missis.db"
	created := newTicket(t, store, "structured values")
	ref := created["ref"].(string)

	code := runMissis(t, store, "set", "--json", ref+"/code", "--kind", "code-ref", "--data-json", `{"Repository":"example/repo","Commit":"abc123","Path":"main.go"}`)
	if code.code != 0 {
		t.Fatalf("CodeRef set failed: %d %s", code.code, code.stderr)
	}
	git := runMissis(t, store, "set", "--json", ref+"/git", "--kind", "git-ref", "--data-json", `{"Repository":"example/repo","Branch":"main"}`)
	if git.code != 0 {
		t.Fatalf("GitRef set failed: %d %s", git.code, git.stderr)
	}
	image := runMissis(t, store, "set", "--json", ref+"/image", "--kind", "image", "--data-json", `{"kind":"image","uri":"artifact:sha256:demo","alt":"diagram"}`)
	if image.code != 0 {
		t.Fatalf("image set failed: %d %s", image.code, image.stderr)
	}
	externalJSON := `{"version":"external-ref-v1","store_id":"store:v1:sha256:120311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867","namespace":"missis","kind":"ticket","entity_id":"ticket:foreign","observation":{"current_event_id":"event:observed"},"display_hint":"foreign#7"}`
	external := runMissis(t, store, "set", "--json", ref+"/external", "--kind", "external-ref", "--data-json", externalJSON)
	if external.code != 0 {
		t.Fatalf("ExternalRef set failed: %d stdout=%s stderr=%s", external.code, external.stdout, external.stderr)
	}
	malicious := runMissis(t, store, "set", "--json", ref+"/malicious", "--kind", "external-ref", "--data-json", `{"version":"external-ref-v1","store_id":"store:v1:sha256:120311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867","namespace":"missis","kind":"ticket","entity_id":"ticket:foreign","locator":"file:///tmp/foreign.db"}`)
	if malicious.code == 0 || !strings.Contains(malicious.stdout, "unknown field") {
		t.Fatalf("malicious ExternalRef accepted: code=%d stdout=%q stderr=%q", malicious.code, malicious.stdout, malicious.stderr)
	}

	shown := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	parts := shown["parts"].(map[string]any)
	codePart := parts["code"].(map[string]any)
	codeValue := codePart["value"].(map[string]any)
	if codeValue["Path"] != "main.go" || codeValue["Commit"] != "abc123" {
		t.Fatalf("CodeRef value = %#v", codeValue)
	}
	gitValue := parts["git"].(map[string]any)["value"].(map[string]any)
	if gitValue["Branch"] != "main" {
		t.Fatalf("GitRef value = %#v", gitValue)
	}
	imageValue := parts["image"].(map[string]any)["value"].(map[string]any)
	if imageValue["uri"] != "artifact:sha256:demo" {
		t.Fatalf("media value = %#v", imageValue)
	}
	externalValue := parts["external"].(map[string]any)["value"].(map[string]any)
	if externalValue["entity_id"] != "ticket:foreign" || externalValue["display_hint"] != "foreign#7" {
		t.Fatalf("ExternalRef value = %#v", externalValue)
	}
}
