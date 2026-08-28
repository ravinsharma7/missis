package model

import (
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/externalref"
)

func TestValidateBuiltInValueRejectsMalformedStructuredReferences(t *testing.T) {
	lineStart, lineEnd := 8, 3
	if err := ValidateBuiltInValue(Value{Kind: ValueKindCodeRef, Data: CodeRef{
		Repository: "repo", Commit: "abc", Path: "main.go", StartLine: &lineStart, EndLine: &lineEnd,
	}}); err == nil || !strings.Contains(err.Error(), "reversed") {
		t.Fatalf("CodeRef error = %v", err)
	}
	if err := ValidateBuiltInValue(Value{Kind: ValueKindGitRef, Data: GitRef{Repository: "repo"}}); err == nil {
		t.Fatal("GitRef without selector was accepted")
	}
	if err := ValidateBuiltInValue(Value{Kind: ValueKindImage, Data: nil}); err == nil {
		t.Fatal("media without descriptor was accepted")
	}
}

func TestValidateExternalReferenceValueIsStrict(t *testing.T) {
	value := Value{Kind: ValueKindExternalRef, Data: map[string]any{
		"version": externalref.VersionV1, "store_id": "store:v1:sha256:120311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867",
		"namespace": "missis", "kind": "ticket", "entity_id": "ticket:x",
	}}
	coerced, err := CoerceBuiltInValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBuiltInValue(coerced); err != nil {
		t.Fatal(err)
	}
	value.Data.(map[string]any)["locator"] = "file:///tmp/foreign.db"
	if _, err := CoerceBuiltInValue(value); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("locator error = %v", err)
	}
}

func TestValidateBuiltInValueAcceptsArtifactAndMedia(t *testing.T) {
	artifactRef := Ref{Kind: KindArtifact, Entity: "artifact:sha256:" + strings.Repeat("a", 64)}
	if err := ValidateBuiltInValue(Value{Kind: ValueKindArtifact, Data: ArtifactDescriptor{Ref: artifactRef, MediaType: "image/png", Size: 12}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBuiltInValue(Value{Kind: ValueKindImage, Data: MediaDescriptor{Kind: ValueKindImage, URI: artifactRef.Entity, MediaType: "image/png"}}); err != nil {
		t.Fatal(err)
	}
}
