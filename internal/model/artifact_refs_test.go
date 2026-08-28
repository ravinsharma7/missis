package model

import (
	"strings"
	"testing"
)

func TestAcceptedArtifactReferencesNamesEverySemanticLocation(t *testing.T) {
	// covers PH1-ART-001
	ref := Ref{Kind: KindArtifact, Entity: "artifact:sha256:" + strings.Repeat("a", 64)}
	before := Value{Ref: &ref}
	after := Value{Data: MediaDescriptor{Kind: ValueKindImage, URI: ref.Entity}}
	event := Event{
		ID:      "event:artifact-locations",
		Stream:  ref,
		Target:  ref,
		Sources: []SourceRef{{Ref: ref}},
		Inputs:  []Ref{ref},
		Causes:  []Ref{ref},
		Effects: []Effect{{Ref: &ref, Evidence: []Ref{ref}, Before: &before, After: &after}},
		Value: Value{Data: InlineSequence{Items: []InlineItem{
			{Kind: InlineArtifact, Data: ArtifactDescriptor{Ref: ref}},
			{Kind: InlineImage, Data: MediaDescriptor{Kind: ValueKindImage, URI: ref.Entity}},
		}}},
	}
	got, err := AcceptedArtifactReferences(event)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"stream", "target", "sources[0]", "inputs[0]", "causes[0]",
		"effects[0].ref", "effects[0].evidence[0]", "effects[0].before.ref",
		"effects[0].after.data.uri", "value.data.items[0].data.ref", "value.data.items[1].data.uri",
	}
	if len(got) != len(want) {
		t.Fatalf("occurrences=%#v, want locations=%#v", got, want)
	}
	for index := range want {
		if got[index].Location != want[index] || got[index].Ref != ref.Entity || !got[index].Managed || got[index].EventID != event.ID {
			t.Fatalf("occurrence[%d]=%#v, want location=%q", index, got[index], want[index])
		}
	}
}

func TestAcceptedArtifactReferencesReportsNamedNonCASSourceWithoutGuessingBytes(t *testing.T) {
	event := Event{ID: "event:named-source", Sources: []SourceRef{{Ref: Ref{Kind: KindArtifact, Entity: "artifact:specs/report.md"}}}}
	got, err := AcceptedArtifactReferences(event)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Ref != "artifact:specs/report.md" || got[0].Managed || got[0].Location != "sources[0]" {
		t.Fatalf("occurrences=%#v", got)
	}
}

func TestAcceptedArtifactReferencesRejectsMalformedSemanticReference(t *testing.T) {
	event := Event{ID: "event:bad-artifact", Value: Value{Ref: &Ref{Kind: KindArtifact, Entity: "artifact:sha256:not-a-digest"}}}
	if _, err := AcceptedArtifactReferences(event); err == nil || !strings.Contains(err.Error(), "event:bad-artifact value.ref") {
		t.Fatalf("error=%v", err)
	}
}
