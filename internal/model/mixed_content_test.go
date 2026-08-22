package model

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMixedContentIsARecursivePartTreeWithTypedPayloads(t *testing.T) {
	ticket := TicketID("ticket:mixed-content")
	stream := Ref{Kind: KindTicket, Entity: string(ticket)}
	actor := ActorRef{Kind: "user", ID: "user:1"}
	base := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	artifactRef := Ref{Kind: KindArtifact, Entity: "artifact:sha256:" + strings.Repeat("a", 64)}
	evidenceID := PartID("part:evidence")

	codePath := "internal/model/model.go"
	gitCommit := "abc123"
	audio := Value{Kind: ValueKindAudio, Data: MediaDescriptor{
		Kind:      ValueKindAudio,
		URI:       artifactRef.Entity,
		MediaType: "audio/mpeg",
		Alt:       "spoken explanation",
	}}
	events := []Event{
		mixedPartEvent(stream, "event:evidence", evidenceID, []string{"evidence"}, nil,
			Value{Kind: ValueKindMarkdown, Text: "Explanation with a screenshot and references."}, artifactRef, actor, base, 1),
		mixedPartEvent(stream, "event:image", "part:image", []string{"evidence", "image"}, &evidenceID,
			Value{Kind: ValueKindImage, Data: MediaDescriptor{Kind: ValueKindImage, URI: artifactRef.Entity, MediaType: "image/png", Alt: "screenshot"}}, artifactRef, actor, base, 2),
		mixedPartEvent(stream, "event:artifact", "part:artifact", []string{"evidence", "artifact"}, &evidenceID,
			Value{Kind: ValueKindArtifact, Data: ArtifactDescriptor{Ref: artifactRef, MediaType: "image/png", Size: 42}}, artifactRef, actor, base, 3),
		mixedPartEvent(stream, "event:code", "part:code", []string{"evidence", "code"}, &evidenceID,
			Value{Kind: ValueKindCodeRef, Data: CodeRef{Repository: "github.com/example/project", Commit: gitCommit, Path: codePath}}, artifactRef, actor, base, 4),
		mixedPartEvent(stream, "event:git", "part:git", []string{"evidence", "git"}, &evidenceID,
			Value{Kind: ValueKindGitRef, Data: GitRef{Repository: "github.com/example/project", Commit: &gitCommit}}, artifactRef, actor, base, 5),
		mixedPartEvent(stream, "event:video", "part:video", []string{"evidence", "video"}, &evidenceID,
			Value{Kind: ValueKindVideo, Data: MediaDescriptor{Kind: ValueKindVideo, URI: "https://example.test/demo.mp4", MediaType: "video/mp4"}}, artifactRef, actor, base, 6),
		mixedPartEvent(stream, "event:audio", "part:audio", []string{"evidence", "audio"}, &evidenceID,
			audio, artifactRef, actor, base, 7),
	}

	projection, err := CurrentProjection(events, ticket, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(projection.Parts); got != len(events) {
		t.Fatalf("parts = %d, want %d", got, len(events))
	}
	if got := projection.Parts[evidenceID].Value.Text; got == "" {
		t.Fatal("markdown explanation was not retained on the parent Part")
	}
	if got := projection.Parts["part:code"].Value.Data.(CodeRef); got.Commit != gitCommit || got.Path != codePath {
		t.Fatalf("code reference = %+v", got)
	}
	if got := projection.Parts["part:git"].Value.Data.(GitRef); got.Commit == nil || *got.Commit != gitCommit {
		t.Fatalf("git reference = %+v", got)
	}
	if got := projection.Parts["part:audio"].Value.Data.(MediaDescriptor); got.Kind != ValueKindAudio {
		t.Fatalf("audio descriptor = %+v", got)
	}
	if got := projection.Parts["part:artifact"].Value.Data.(ArtifactDescriptor); !reflect.DeepEqual(got.Ref, artifactRef) || got.Size != 42 {
		t.Fatalf("artifact descriptor = %+v", got)
	}
	if got := projection.Parts["part:video"].Value.Data.(MediaDescriptor); got.URI == "" {
		t.Fatal("video URI was lost")
	}
	if got := projection.Parts["part:image"].Sources[0].Ref; !reflect.DeepEqual(got, artifactRef) {
		t.Fatalf("image provenance = %+v, want %+v", got, artifactRef)
	}

	canonical, err := CanonicalEventBytesV1(events[1])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(canonical, []byte(`"Kind":"image"`)) || bytes.Contains(canonical, []byte("BINARY_BYTES")) {
		t.Fatalf("canonical event did not contain only typed metadata: %s", canonical)
	}
}

func mixedPartEvent(stream Ref, id string, partID PartID, path []string, parent *PartID, value Value, source Ref, actor ActorRef, at time.Time, sequence uint64) Event {
	var parentRef *Ref
	if parent != nil {
		ref := Ref{Kind: KindPart, Entity: string(*parent)}
		parentRef = &ref
		value.Ref = parentRef
	}
	return Event{
		ID:          EventID(id),
		Stream:      stream,
		Sequence:    sequence,
		Operation:   OpCreatePart,
		Target:      Ref{Kind: KindPart, Entity: string(partID), Path: path},
		Value:       value,
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       actor,
		Sources:     []SourceRef{{Ref: source, MediaType: "application/octet-stream"}},
	}
}
