package missis_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestOrderedInlineContentRoundTripsThroughAPIAndMarkdownExport(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "missis.db")
	svc, err := application.OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	commit := "abc123"
	artifactURI := "artifact:sha256:" + strings.Repeat("b", 64)
	sequence := model.InlineSequence{Items: []model.InlineItem{
		{Kind: model.InlineMarkdownText, Text: "prose"},
		{Kind: model.InlineImage, Data: model.MediaDescriptor{Kind: model.ValueKindImage, URI: artifactURI, MediaType: "image/png"}},
		{Kind: model.InlineMarkdownText, Text: "more prose"},
		{Kind: model.InlineCodeRef, Data: model.CodeRef{Repository: "github.com/example/missis", Commit: commit, Path: "main.go"}},
		{Kind: model.InlineAudio, Data: model.MediaDescriptor{Kind: model.ValueKindAudio, URI: artifactURI, MediaType: "audio/mpeg"}},
		{Kind: model.InlineVideo, Data: model.MediaDescriptor{Kind: model.ValueKindVideo, URI: artifactURI, MediaType: "video/mp4"}},
		{Kind: model.InlineGitRef, Data: model.GitRef{Repository: "github.com/example/missis", Commit: &commit}},
		{Kind: model.InlineRawMarkdown, Text: "![inert](https://example.test/no-fetch)"},
	}}
	inlineMarkdown, err := model.InlineSequenceMarkdown(sequence)
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.ImportMarkdown(ctx, missis.RequestContext{Actor: "test"}, missis.ImportOptions{
		Content:  "# Inline\n\n## content\n\n" + inlineMarkdown,
		Artifact: "inline.md",
	})
	if err != nil {
		t.Fatal(err)
	}

	projection, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := projection.Parts["content"].Value.(model.InlineSequence)
	if !ok {
		t.Fatalf("stored inline value = %#v", projection.Parts["content"].Value)
	}
	coerced, err := model.CoerceInlineSequence(sequence)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, coerced) {
		t.Fatalf("stored inline sequence = %#v, want %#v", stored, coerced)
	}
	exported, err := client.ExportMarkdown(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported, "missis-inline") || !strings.Contains(exported, "no-fetch") {
		t.Fatalf("export did not retain explicit inert inline data: %s", exported)
	}
	if !strings.Contains(exported, "missis-part") {
		t.Fatalf("export did not retain the content Part identity: %s", exported)
	}
	parsed, err := model.ParseInlineSequenceMarkdown(exported)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, coerced) {
		t.Fatalf("export/re-import changed inline sequence = %#v, want %#v", parsed, coerced)
	}
	beforeID := projection.Parts["content"].ID
	reimported, err := client.ReimportMarkdown(ctx, missis.RequestContext{Actor: "test"}, missis.ImportOptions{
		Ref:      created.Ref,
		Content:  exported,
		Artifact: "inline.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reimported.Value != 0 {
		t.Fatalf("identity-preserving reimport wrote unexpected events: %+v", reimported)
	}
	afterReimport, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := afterReimport.Parts["content"].ID; got != beforeID {
		t.Fatalf("content Part identity changed on reimport: got %q, want %q", got, beforeID)
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := application.OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	reopenedClient := missis.NewClient(reopened)
	defer reopenedClient.Close()
	if err := reopenedClient.RebuildProjection(ctx); err != nil {
		t.Fatal(err)
	}
	reopenedProjection, err := reopenedClient.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reopenedSequence, ok := reopenedProjection.Parts["content"].Value.(model.InlineSequence)
	if !ok || !reflect.DeepEqual(reopenedSequence, coerced) {
		t.Fatalf("reopened inline sequence = %#v, want %#v", reopenedProjection.Parts["content"].Value, coerced)
	}
}
