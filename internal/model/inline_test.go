package model

import (
	"embed"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/inline_sequence.golden.json
var inlineGolden embed.FS

func TestInlineSequenceGoldenMarkdownRoundTrip(t *testing.T) {
	// covers N106
	artifactRef := Ref{Kind: KindArtifact, Entity: "artifact:sha256:" + strings.Repeat("a", 64)}
	commit := "abc123"
	sequence := InlineSequence{Items: []InlineItem{
		{Kind: InlineMarkdownText, Text: "Introduction\n"},
		{Kind: InlineImage, Data: MediaDescriptor{Kind: ValueKindImage, URI: artifactRef.Entity, MediaType: "image/png"}},
		{Kind: InlineMarkdownText, Text: "The implementation follows."},
		{Kind: InlineCodeRef, Data: CodeRef{Repository: "github.com/example/missis", Commit: commit, Path: "main.go"}},
		{Kind: InlineAudio, Data: MediaDescriptor{Kind: ValueKindAudio, URI: artifactRef.Entity, MediaType: "audio/mpeg"}},
		{Kind: InlineVideo, Data: MediaDescriptor{Kind: ValueKindVideo, URI: artifactRef.Entity, MediaType: "video/mp4"}},
		{Kind: InlineArtifact, Data: ArtifactDescriptor{Ref: artifactRef, MediaType: "application/octet-stream", Size: 42}},
		{Kind: InlineGitRef, Data: GitRef{Repository: "github.com/example/missis", Commit: &commit}},
		{Kind: InlineRawMarkdown, Text: "![ordinary](https://example.test/image.png)"},
	}}
	coerced, err := CoerceInlineSequence(sequence)
	if err != nil {
		t.Fatal(err)
	}
	if coerced.Items[0].ID != "inline-000001" || coerced.Items[8].ID != "inline-000009" {
		t.Fatalf("core-assigned IDs = %+v", coerced.Items)
	}
	if err := ValidateBuiltInValue(Value{Kind: ValueKindInlineSequence, Data: coerced}); err != nil {
		t.Fatal(err)
	}

	encoded, err := json.MarshalIndent(coerced, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := inlineGolden.ReadFile("testdata/inline_sequence.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded) + "\n"; got != string(golden) {
		t.Fatalf("inline snapshot mismatch:\n%s", got)
	}

	markdown, err := InlineSequenceMarkdown(coerced)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseInlineSequenceMarkdown(markdown)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(coerced, parsed) {
		t.Fatalf("inline Markdown round trip changed semantics:\nwant=%#v\ngot=%#v", coerced, parsed)
	}
	ordinary, err := ParseInlineSequenceMarkdown("![ordinary](https://example.test/image.png)\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(ordinary.Items) != 0 {
		t.Fatalf("ordinary Markdown media was promoted: %#v", ordinary)
	}
	code, err := ParseInlineSequenceMarkdown("```markdown\n<!-- missis-inline {\"ID\":\"inline-code\",\"Kind\":\"image\"} -->\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(code.Items) != 0 {
		t.Fatalf("inline marker inside fenced code was promoted: %#v", code)
	}
}

func TestInlineMarkerValidation(t *testing.T) {
	tests := []string{
		"<!-- missis-inline -->\n",
		"<!-- missis-inline {\"Kind\":\"image\"} -->\n",
		"<!-- missis-inline {\"ID\":\"x\",\"Kind\":\"image\",} -->\n",
		"<!-- missis-inline {\"ID\":\"x\",\"Kind\":\"unknown\"} -->\n",
		"<!-- missis-inline {\"ID\":\"x\",\"Kind\":\"image\"} -->\n",
		"<!-- missis-inline {\"ID\":\"x\",\"Kind\":\"image\",\"Data\":{\"kind\":\"image\",\"uri\":\"artifact:sha256:" + strings.Repeat("a", 64) + "\"}} -->\n" +
			"<!-- missis-inline {\"ID\":\"x\",\"Kind\":\"video\",\"Data\":{\"kind\":\"video\",\"uri\":\"artifact:sha256:" + strings.Repeat("b", 64) + "\"}} -->\n",
	}
	for _, markdown := range tests {
		if _, err := ParseInlineSequenceMarkdown(markdown); err == nil {
			t.Fatalf("expected inline marker validation error for %q", markdown)
		}
	}
}

func TestInlineMarkerInsideIndentedCodeRemainsInert(t *testing.T) {
	markdown := "    <!-- missis-inline {\"ID\":\"literal\",\"Kind\":\"image\"} -->\n"
	sequence, err := ParseInlineSequenceMarkdown(markdown)
	if err != nil {
		t.Fatal(err)
	}
	if len(sequence.Items) != 0 {
		t.Fatalf("indented code marker was promoted: %#v", sequence)
	}
}
