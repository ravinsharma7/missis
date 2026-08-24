package blackbox

import "testing"

func TestMediaValueKindsRoundTrip(t *testing.T) {
	t.Parallel()
	store := t.TempDir() + "/missis.db"
	created := newTicket(t, store, "media descriptor")
	ref := created["ref"].(string)

	image := runMissis(t, store, "set", "--json", ref+"/screenshot", "https://example.test/screenshot.png", "--kind", "image")
	if image.code != 0 {
		t.Fatalf("image set failed: %d %s", image.code, image.stderr)
	}
	video := runMissis(t, store, "set", "--json", ref+"/recording", "artifact:sha256:demo-video", "--kind", "video")
	if video.code != 0 {
		t.Fatalf("video set failed: %d %s", video.code, video.stderr)
	}
	embed := runMissis(t, store, "set", "--json", ref+"/player", `<iframe src="https://example.test/player"></iframe>`, "--kind", "embed")
	if embed.code != 0 {
		t.Fatalf("embed set failed: %d %s", embed.code, embed.stderr)
	}

	shown := mustJSON(t, runMissis(t, store, "show", "--json", ref))
	parts := shown["parts"].(map[string]any)
	for path, wantKind := range map[string]string{
		"screenshot": "image",
		"recording":  "video",
		"player":     "embed",
	} {
		part, ok := parts[path].(map[string]any)
		if !ok {
			t.Fatalf("part %s has unexpected shape: %#v", path, parts[path])
		}
		if got := part["value_kind"]; got != wantKind {
			t.Errorf("part %s kind = %v, want %s", path, got, wantKind)
		}
	}
}
