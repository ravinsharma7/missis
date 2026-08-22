package media

import (
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
)

func TestParseExplicitImageURI(t *testing.T) {
	value, ok := Parse(model.ValueKindImage, "https://example.test/diagram.png")
	if !ok {
		t.Fatal("image URI was not parsed")
	}
	if value.URI != "https://example.test/diagram.png" || value.Kind != model.ValueKindImage {
		t.Fatalf("parsed image = %+v", value)
	}
	lines := FallbackLines(value)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"[image] <no alt text>", "uri: https://example.test/diagram.png", "terminal image protocol not enabled"} {
		if !strings.Contains(joined, want) {
			t.Errorf("fallback missing %q:\n%s", want, joined)
		}
	}
}

func TestParseStructuredVideoDescriptor(t *testing.T) {
	value, ok := Parse(model.ValueKindVideo, map[string]any{
		"uri":        "artifact:sha256:video",
		"media_type": "video/mp4",
		"alt":        "Demo recording",
		"poster":     "artifact:sha256:poster",
	})
	if !ok {
		t.Fatal("video descriptor was not parsed")
	}
	if value.MediaType != "video/mp4" || value.PosterURI == "" || value.Alt != "Demo recording" {
		t.Fatalf("parsed video = %+v", value)
	}
	if got := strings.Join(FallbackLines(value), "\n"); !strings.Contains(got, "external player required") {
		t.Fatalf("video fallback missing playback notice: %s", got)
	}
}

func TestParseAudioDescriptorUsesSafeFallback(t *testing.T) {
	value, ok := Parse(model.ValueKindAudio, model.MediaDescriptor{
		Kind:      model.ValueKindAudio,
		URI:       "artifact:sha256:audio",
		MediaType: "audio/mpeg",
		Alt:       "voice note",
	})
	if !ok || value.Kind != model.ValueKindAudio {
		t.Fatalf("audio descriptor = %+v, parsed=%v", value, ok)
	}
	got := strings.Join(FallbackLines(value), "\n")
	if !strings.Contains(got, "playback: external player required") || strings.Contains(got, "<audio") {
		t.Fatalf("audio fallback = %s", got)
	}
}

func TestInlineIframeIsNeverEmitted(t *testing.T) {
	raw := `<iframe src="https://player.example.test/demo" allowfullscreen></iframe>`
	value, ok := Parse(model.ValueKindEmbed, raw)
	if !ok || !value.Inline || value.URI != "https://player.example.test/demo" {
		t.Fatalf("iframe descriptor = %+v, parsed=%v", value, ok)
	}
	got := strings.Join(FallbackLines(value), "\n")
	if strings.Contains(strings.ToLower(got), "<iframe") {
		t.Fatalf("fallback leaked iframe markup: %s", got)
	}
	if !strings.Contains(got, "not executed") {
		t.Fatalf("fallback missing safety notice: %s", got)
	}
}

func TestStructuredDescriptorUsesExplicitKind(t *testing.T) {
	value, ok := Parse(model.ValueKindJSON, map[string]any{
		"kind": "image",
		"uri":  "artifact:sha256:image",
	})
	if !ok || value.Kind != model.ValueKindImage {
		t.Fatalf("structured media kind = %+v, parsed=%v", value, ok)
	}
}

func TestUnsupportedValueDoesNotBecomeMedia(t *testing.T) {
	if _, ok := Parse(model.ValueKindArtifact, "artifact:sha256:generic"); ok {
		t.Fatal("generic artifact was incorrectly inferred as media")
	}
}
