package blackbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type alphaSmokeClock struct{}

func (alphaSmokeClock) Now() time.Time { return time.Now().UTC() }

func TestLocalAlphaEndToEndWorkflow(t *testing.T) {
	storeDir := t.TempDir()
	store := filepath.Join(storeDir, "project", "missis.db")
	artifactRoot := filepath.Join(storeDir, "artifacts")
	env := []string{"MISSIS_ARTIFACT_STORE=" + artifactRoot}
	ctx := context.Background()

	markdown := filepath.Join(storeDir, "explanation.md")
	markdownBody := "# Alpha workflow\n\n## Evidence\n\nThe image syntax remains raw: ![remote](https://example.test/no-fetch.png).\n\n```markdown\n## Not a Part\n```\n"
	if err := os.MkdirAll(filepath.Dir(store), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markdown, []byte(markdownBody), 0o600); err != nil {
		t.Fatal(err)
	}

	created := mustJSON(t, runMissisWithEnv(t, store, "", env, "new", "--json", "local alpha workflow"))
	ref := created["ref"].(string)
	if result := runMissisWithEnv(t, store, "", env, "set", "--json", ref, "--from", markdown); result.code != 0 {
		t.Fatalf("Markdown import failed: %d %s", result.code, result.stderr)
	}

	attachments := []struct {
		name      string
		mediaType string
		content   string
	}{
		{name: "image.png", mediaType: "image/png", content: "alpha image"},
		{name: "audio.mp3", mediaType: "audio/mpeg", content: "alpha audio"},
		{name: "video.mp4", mediaType: "video/mp4", content: "alpha video"},
		{name: "payload.bin", mediaType: "application/octet-stream", content: "alpha artifact"},
	}
	artifactRefs := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		file := filepath.Join(storeDir, attachment.name)
		if err := os.WriteFile(file, []byte(attachment.content), 0o600); err != nil {
			t.Fatal(err)
		}
		result := runMissisWithEnv(t, store, "", env, "set", "--json", ref+"/evidence", "--attach", file, "--media-type", attachment.mediaType)
		if result.code != 0 {
			t.Fatalf("attach %s failed: %d %s", attachment.name, result.code, result.stderr)
		}
		body := mustJSON(t, result)
		artifactRef, ok := body["artifact"].(string)
		if !ok || artifactRef == "" {
			t.Fatalf("attach %s returned no artifact reference: %v", attachment.name, body)
		}
		artifactRefs = append(artifactRefs, artifactRef)
	}

	if result := runMissisWithEnv(t, store, "", env, "set", "--json", ref+"/evidence/code", "--kind", "code-ref", "--data-json", "{\"Repository\":\"example/repo\",\"Commit\":\"abc123\",\"Path\":\"main.go\"}"); result.code != 0 {
		t.Fatalf("CodeRef failed: %d %s", result.code, result.stderr)
	}
	if result := runMissisWithEnv(t, store, "", env, "set", "--json", ref+"/evidence/git", "--kind", "git-ref", "--data-json", "{\"Repository\":\"example/repo\",\"Branch\":\"main\"}"); result.code != 0 {
		t.Fatalf("GitRef failed: %d %s", result.code, result.stderr)
	}

	view := mustJSON(t, runMissisWithEnv(t, store, "", env, "show", "--json", ref))
	parts, ok := view["parts"].(map[string]any)
	if !ok {
		t.Fatalf("parts missing from initial projection: %v", view)
	}
	evidence, ok := parts["evidence"].(map[string]any)
	if !ok {
		t.Fatalf("evidence part missing: %v", parts)
	}
	evidenceValue, _ := evidence["value"].(string)
	if !strings.Contains(evidenceValue, "https://example.test/no-fetch.png") || !strings.Contains(evidenceValue, "Not a Part") {
		t.Fatalf("Markdown was not preserved as inert data: %q", evidenceValue)
	}
	for _, path := range []string{"evidence/image", "evidence/audio", "evidence/video", "evidence/payload", "evidence/code", "evidence/git"} {
		if _, ok := parts[path]; !ok {
			t.Fatalf("expected mixed-content child %q in %v", path, parts)
		}
	}

	svc, err := application.OpenPathWithClockAndArtifactRoot(store, alphaSmokeClock{}, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	projection, err := client.ShowTicket(ctx, ref, missis.ShowOptions{})
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	audio, ok := projection.Parts["evidence/audio"]
	if !ok {
		_ = client.Close()
		t.Fatalf("audio child missing after SDK reopen: %+v", projection.Parts)
	}
	if _, ok := projection.Parts["evidence/video"]; !ok {
		_ = client.Close()
		t.Fatalf("video child missing after SDK reopen: %+v", projection.Parts)
	}
	if _, err := client.Set(ctx, missis.RequestContext{Actor: "alpha-smoke"}, missis.MovePart{
		Target: ref + "/evidence/video",
		Parent: "part:" + evidence["id"].(string),
		Before: "part:" + audio.ID,
		Reason: "verify ordered traversal",
	}); err != nil {
		_ = client.Close()
		t.Fatalf("reorder video before audio: %v", err)
	}
	projection, err = client.ShowTicket(ctx, ref, missis.ShowOptions{})
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	if partIndex(projection.PartOrder, "evidence/video") >= partIndex(projection.PartOrder, "evidence/audio") {
		_ = client.Close()
		t.Fatalf("reorder was not reflected in traversal: %v", projection.PartOrder)
	}
	if err := client.CheckConsistency(ctx); err != nil {
		_ = client.Close()
		t.Fatalf("source consistency: %v", err)
	}

	backup := filepath.Join(storeDir, "backup", "alpha.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		_ = client.Close()
		t.Fatalf("SDK backup: %v", err)
	}
	manifest, err := client.Manifest(ctx)
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	if err := client.VerifyRestore(ctx, backup, manifest); err != nil {
		_ = client.Close()
		t.Fatalf("backup verification: %v", err)
	}
	restoredDB := filepath.Join(storeDir, "restored", "missis.db")
	restoredRoot := filepath.Join(storeDir, "restored", "artifacts")
	if err := client.RestoreWithOptions(ctx, backup, restoredDB, missis.RestoreOptions{ArtifactRoot: restoredRoot}); err != nil {
		_ = client.Close()
		t.Fatalf("restore: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	restoredSvc, err := application.OpenPathWithClockAndArtifactRoot(restoredDB, alphaSmokeClock{}, restoredRoot)
	if err != nil {
		t.Fatal(err)
	}
	restoredClient := missis.NewClient(restoredSvc)
	defer restoredClient.Close()
	if err := restoredClient.RebuildProjection(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restoredClient.CheckConsistency(ctx); err != nil {
		t.Fatalf("restored consistency: %v", err)
	}
	restoredProjection, err := restoredClient.ShowTicket(ctx, ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if partIndex(restoredProjection.PartOrder, "evidence/video") >= partIndex(restoredProjection.PartOrder, "evidence/audio") {
		t.Fatalf("restored order changed: %v", restoredProjection.PartOrder)
	}
	if _, ok := restoredProjection.Parts["evidence/code"].Value.(model.CodeRef); !ok {
		t.Fatalf("restored CodeRef = %#v", restoredProjection.Parts["evidence/code"].Value)
	}
	if _, ok := restoredProjection.Parts["evidence/git"].Value.(model.GitRef); !ok {
		t.Fatalf("restored GitRef = %#v", restoredProjection.Parts["evidence/git"].Value)
	}
	for _, rawRef := range artifactRefs {
		ref, err := artifact.ParseRef(rawRef)
		if err != nil {
			t.Fatalf("parse restored artifact reference %q: %v", rawRef, err)
		}
		exists, err := restoredSvc.ArtifactStore().Exists(ctx, ref)
		if err != nil || !exists {
			t.Fatalf("restored artifact %s exists=%v err=%v", rawRef, exists, err)
		}
	}

	toolBackup := filepath.Join(storeDir, "backup", "tools.db")
	if result := runMissisTools(t, store, env, "backup", toolBackup); result.code != 0 || result.stderr != "" {
		t.Fatalf("missis-tools backup: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	if result := runMissisTools(t, "", nil, "backup", "verify", toolBackup); result.code != 0 || !strings.Contains(result.stdout, "state=complete") || result.stderr != "" {
		t.Fatalf("missis-tools backup verify: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
}

func TestLocalAlphaToolSurfaceAndReadinessDocumentation(t *testing.T) {
	help := runMissisTools(t, "", nil, "--help")
	if help.code != 0 || help.stderr != "" {
		t.Fatalf("missis-tools help: code=%d stderr=%q", help.code, help.stderr)
	}
	for _, want := range []string{"backup verify", "backup cleanup", "artifacts migrate", "artifacts gc"} {
		if !strings.Contains(help.stdout, want) {
			t.Errorf("missis-tools help missing %q", want)
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "local-alpha-readiness.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Implementation evidence", "Automated evidence", "MISSIS_ARTIFACT_STORE", "#101", "#102", "#103", "#104"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("local alpha checklist missing %q", want)
		}
	}
}

func partIndex(order []string, path string) int {
	for i, candidate := range order {
		if candidate == path {
			return i
		}
	}
	return len(order) + 1
}
