package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValueUnmarshalRestoresStructuredPayloads(t *testing.T) {
	commit := "abc123"
	values := []Value{
		{Kind: ValueKindCodeRef, Data: CodeRef{Repository: "example/repo", Commit: commit, Path: "main.go"}},
		{Kind: ValueKindGitRef, Data: GitRef{Repository: "example/repo", Commit: &commit}},
		{Kind: ValueKindArtifact, Data: ArtifactDescriptor{Ref: Ref{Kind: KindArtifact, Entity: "artifact:sha256:" + strings.Repeat("a", 64)}, Size: 4}},
		{Kind: ValueKindImage, Data: MediaDescriptor{Kind: ValueKindImage, URI: "artifact:sha256:" + strings.Repeat("b", 64)}},
	}
	for _, want := range values {
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		var got Value
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		switch want.Kind {
		case ValueKindCodeRef:
			if got.Data.(CodeRef).Path != "main.go" {
				t.Fatalf("CodeRef = %#v", got.Data)
			}
		case ValueKindGitRef:
			if got.Data.(GitRef).Commit == nil || *got.Data.(GitRef).Commit != commit {
				t.Fatalf("GitRef = %#v", got.Data)
			}
		case ValueKindArtifact:
			if got.Data.(ArtifactDescriptor).Size != 4 {
				t.Fatalf("ArtifactDescriptor = %#v", got.Data)
			}
		case ValueKindImage:
			if got.Data.(MediaDescriptor).Kind != ValueKindImage {
				t.Fatalf("MediaDescriptor = %#v", got.Data)
			}
		}
	}
}
