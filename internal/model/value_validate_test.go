package model

import (
	"strings"
	"testing"
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

func TestValidateBuiltInValueAcceptsArtifactAndMedia(t *testing.T) {
	artifactRef := Ref{Kind: KindArtifact, Entity: "artifact:sha256:" + strings.Repeat("a", 64)}
	if err := ValidateBuiltInValue(Value{Kind: ValueKindArtifact, Data: ArtifactDescriptor{Ref: artifactRef, MediaType: "image/png", Size: 12}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBuiltInValue(Value{Kind: ValueKindImage, Data: MediaDescriptor{Kind: ValueKindImage, URI: artifactRef.Entity, MediaType: "image/png"}}); err != nil {
		t.Fatal(err)
	}
}
