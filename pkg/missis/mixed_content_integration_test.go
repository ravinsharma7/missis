package missis_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestMixedContentRoundTripsOrderedTypedChildren(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "missis.db")
	svc, err := application.OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)

	created, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "Mixed content"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Ingest(ctx, missis.RequestContext{Actor: "test"}, missis.IngestOptions{
		Operation:  "import-markdown",
		Target:     created.ID,
		MediaType:  "text/markdown",
		SourceName: "explanation.md",
		Content:    strings.NewReader("## Evidence\n\nProse with ![image](https://example.test/image.png).\n\n```markdown\n## Not a Part\n```\n"),
	})
	if err != nil {
		t.Fatal(err)
	}

	attachments := []struct {
		name      string
		mediaType string
		content   string
	}{
		{name: "image.png", mediaType: "image/png", content: "image"},
		{name: "audio.mp3", mediaType: "audio/mpeg", content: "audio"},
		{name: "video.mp4", mediaType: "video/mp4", content: "video"},
		{name: "archive.bin", mediaType: "application/octet-stream", content: "artifact"},
	}
	for _, attachment := range attachments {
		if _, err := client.Ingest(ctx, missis.RequestContext{Actor: "test"}, missis.IngestOptions{
			Target:     created.Ref + "/evidence",
			MediaType:  attachment.mediaType,
			SourceName: attachment.name,
			Content:    strings.NewReader(attachment.content),
		}); err != nil {
			t.Fatalf("attach %s: %v", attachment.name, err)
		}
	}
	commit := "abc123"
	if _, err := client.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValueData{
		Target: created.Ref + "/evidence/code",
		Kind:   model.ValueKindCodeRef,
		Data:   model.CodeRef{Repository: "github.com/example/project", Commit: commit, Path: "main.go"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValueData{
		Target: created.Ref + "/evidence/git",
		Kind:   model.ValueKindGitRef,
		Data:   model.GitRef{Repository: "github.com/example/project", Commit: &commit},
	}); err != nil {
		t.Fatal(err)
	}

	projection, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := projection.Parts["evidence"]
	if !ok {
		t.Fatalf("evidence Part missing: %+v", projection.Parts)
	}
	if body, ok := evidence.Value.(string); !ok || !strings.Contains(body, "![image]") || !strings.Contains(body, "Not a Part") {
		t.Fatalf("Markdown was not retained as inert raw data: %#v", evidence.Value)
	}
	if _, ok := projection.Parts["evidence/not-a-part"]; ok {
		t.Fatal("heading inside fenced Markdown was parsed as a Part")
	}
	if _, ok := projection.Parts["evidence/image"].Value.(model.MediaDescriptor); !ok {
		t.Fatalf("image value = %#v", projection.Parts["evidence/image"].Value)
	}
	if _, ok := projection.Parts["evidence/audio"].Value.(model.MediaDescriptor); !ok {
		t.Fatalf("audio value = %#v", projection.Parts["evidence/audio"].Value)
	}
	if _, ok := projection.Parts["evidence/video"].Value.(model.MediaDescriptor); !ok {
		t.Fatalf("video value = %#v", projection.Parts["evidence/video"].Value)
	}
	if _, ok := projection.Parts["evidence/code"].Value.(model.CodeRef); !ok {
		t.Fatalf("code value = %#v", projection.Parts["evidence/code"].Value)
	}
	if _, ok := projection.Parts["evidence/git"].Value.(model.GitRef); !ok {
		t.Fatalf("git value = %#v", projection.Parts["evidence/git"].Value)
	}

	videoID := projection.Parts["evidence/video"].ID
	audioID := projection.Parts["evidence/audio"].ID
	if _, err := client.Set(ctx, missis.RequestContext{Actor: "test"}, missis.MovePart{
		Target: created.Ref + "/evidence/video",
		Parent: "part:" + evidence.ID,
		Before: "part:" + audioID,
		Reason: "preserve source sequence",
	}); err != nil {
		t.Fatal(err)
	}
	if videoID == "" {
		t.Fatal("video Part has no ID")
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := application.OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	reopenedClient := missis.NewClient(reopened)
	defer reopenedClient.Close()
	if err := reopenedClient.RebuildProjection(ctx); err != nil {
		t.Fatal(err)
	}
	projection, err = reopenedClient.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	videoIndex, audioIndex := -1, -1
	for i, path := range projection.PartOrder {
		if path == "evidence/video" {
			videoIndex = i
		}
		if path == "evidence/audio" {
			audioIndex = i
		}
	}
	if videoIndex < 0 || audioIndex < 0 || videoIndex >= audioIndex {
		t.Fatalf("ordered children = %v, video=%d audio=%d", projection.PartOrder, videoIndex, audioIndex)
	}
	if projection.Parts["evidence/video"].OrderKey == "" || projection.Parts["evidence/audio"].OrderKey == "" {
		t.Fatal("ordered children lost their persisted order keys")
	}
	if err := reopenedClient.CheckConsistency(ctx); err != nil {
		t.Fatal(err)
	}
}
