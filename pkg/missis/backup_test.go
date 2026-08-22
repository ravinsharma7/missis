package missis_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestBackupManifestRestoreVerify(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer svc.Close()
	ctx := context.Background()
	created, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "Backup me"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValue{Target: created.Ref + "/problem", Value: "content", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}

	manifest, err := client.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.StoreID == "" || manifest.HeadHash == "" || manifest.EventCount < 3 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}

	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	for _, sidecar := range []string{backup + ".manifest.json", backup + ".artifacts", backup + ".complete.json"} {
		if _, err := os.Stat(sidecar); err != nil {
			t.Fatalf("backup sidecar %s: %v", sidecar, err)
		}
	}
	var backupManifest missis.BackupManifest
	manifestData, err := os.ReadFile(backup + ".manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestData, &backupManifest); err != nil {
		t.Fatal(err)
	}
	if backupManifest.Version != missis.BackupManifestVersion || backupManifest.ArtifactMode != missis.BackupArtifactEmbedded {
		t.Fatalf("backup manifest = %+v", backupManifest)
	}
	restoredSvc, err := application.OpenPath(backup)
	if err != nil {
		t.Fatal(err)
	}
	restored := missis.NewClient(restoredSvc)
	got, err := restored.Manifest(ctx)
	restoredSvc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got != manifest {
		t.Fatalf("backup manifest mismatch: %+v != %+v", got, manifest)
	}
	if err := client.VerifyRestore(ctx, backup, manifest); err != nil {
		t.Fatalf("verify backup: %v", err)
	}

	restoreDst := filepath.Join(dir, "restored.db")
	if err := client.RestoreWithOptions(ctx, backup, restoreDst, missis.RestoreOptions{ArtifactRoot: filepath.Join(dir, "restored-artifacts")}); err != nil {
		t.Fatal(err)
	}
	restoredSvc2, err := application.OpenPath(restoreDst)
	if err != nil {
		t.Fatal(err)
	}
	restored2 := missis.NewClient(restoredSvc2)
	defer restored2.Close()
	proj, err := restored2.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Title != "Backup me" || proj.Parts["problem"].Value != "content" {
		t.Fatalf("restored projection = %+v", proj)
	}
}

func TestVersionTwoBackupRequiresCompletionMarker(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(backup + ".complete.json"); err != nil {
		t.Fatal(err)
	}
	if err := client.VerifyRestore(ctx, backup, mustManifest(t, client)); err == nil || !strings.Contains(err.Error(), "completion marker") {
		t.Fatalf("verify without completion marker = %v", err)
	}
}

func TestBackupBundleCopiesArtifactsAndRejectsTampering(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	sourceRoot := filepath.Join(dir, "source-artifacts")
	restoreRoot := filepath.Join(dir, "restore-artifacts")
	svc, err := application.OpenPathWithClockAndArtifactRoot(filepath.Join(dir, "missis.db"), backupTestClock{}, sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	created, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "artifact backup"})
	if err != nil {
		t.Fatal(err)
	}
	ingested, err := client.Ingest(ctx, missis.RequestContext{Actor: "test"}, missis.IngestOptions{
		Target: created.ID, MediaType: "image/png", SourceName: "preview.png", Content: strings.NewReader("png bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	var bundle missis.BackupManifest
	data, err := os.ReadFile(backup + ".manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Artifacts) != 1 || bundle.Artifacts[0].Ref != ingested.Artifact {
		t.Fatalf("bundle artifacts = %+v", bundle.Artifacts)
	}
	if err := client.VerifyRestore(ctx, backup, mustManifest(t, client)); err != nil {
		t.Fatalf("verify complete bundle: %v", err)
	}
	if err := client.RestoreWithOptions(ctx, backup, filepath.Join(dir, "restored.db"), missis.RestoreOptions{ArtifactRoot: restoreRoot}); err != nil {
		t.Fatalf("restore complete bundle: %v", err)
	}
	restoredSvc, err := application.OpenPathWithClockAndArtifactRoot(filepath.Join(dir, "restored.db"), backupTestClock{}, restoreRoot)
	if err != nil {
		t.Fatal(err)
	}
	restoredClient := missis.NewClient(restoredSvc)
	defer restoredClient.Close()
	ref, err := artifact.ParseRef(ingested.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	if exists, err := restoredSvc.ArtifactStore().Exists(ctx, ref); err != nil || !exists {
		t.Fatalf("restored artifact exists=%v err=%v", exists, err)
	}

	digest := strings.TrimPrefix(ingested.Artifact, "artifact:sha256:")
	blob := filepath.Join(backup+".artifacts", "sha256", digest[:2], digest[2:4], digest)
	if err := os.WriteFile(blob, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.VerifyRestore(ctx, backup, bundle.Store); err == nil {
		t.Fatal("expected tampered artifact to fail verification")
	}
	if err := client.RestoreWithOptions(ctx, backup, filepath.Join(dir, "tampered-restore.db"), missis.RestoreOptions{ArtifactRoot: filepath.Join(dir, "tampered-artifacts")}); err == nil {
		t.Fatal("expected tampered artifact restore to fail")
	}
}

func TestLegacyDatabaseOnlyBackupRemainsReadable(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	created, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "legacy.db")
	if err := svc.Store().BackupContext(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(ctx, legacy, filepath.Join(dir, "restored.db")); err != nil {
		t.Fatal(err)
	}
	restored, err := application.OpenPath(filepath.Join(dir, "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err := missis.NewClient(restored).ShowTicket(ctx, created.Ref, missis.ShowOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreUsesArtifactStoreEnvironmentOverride(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	sourceRoot := filepath.Join(dir, "source-artifacts")
	restoreRoot := filepath.Join(dir, "restore-artifacts")
	svc, err := application.OpenPathWithClockAndArtifactRoot(filepath.Join(dir, "missis.db"), backupTestClock{}, sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	created, err := client.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "environment restore"})
	if err != nil {
		t.Fatal(err)
	}
	ingested, err := client.Ingest(ctx, missis.RequestContext{}, missis.IngestOptions{
		Target: created.ID, MediaType: "image/png", SourceName: "image.png", Content: strings.NewReader("image"),
	})
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISSIS_ARTIFACT_STORE", restoreRoot)
	restoredPath := filepath.Join(dir, "restored.db")
	if err := client.Restore(ctx, backup, restoredPath); err != nil {
		t.Fatal(err)
	}
	restoredSvc, err := application.OpenPathWithClock(restoredPath, backupTestClock{})
	if err != nil {
		t.Fatal(err)
	}
	defer restoredSvc.Close()
	if restoredSvc.ArtifactRoot() != restoreRoot {
		t.Fatalf("restored artifact root = %q, want %q", restoredSvc.ArtifactRoot(), restoreRoot)
	}
	ref, err := artifact.ParseRef(ingested.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	if exists, err := restoredSvc.ArtifactStore().Exists(ctx, ref); err != nil || !exists {
		t.Fatalf("restored artifact exists=%v err=%v", exists, err)
	}
}

func TestRestoreRequiresNewDestination(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(ctx, backup, backup); err == nil {
		t.Fatal("expected restore to reject an existing destination")
	} else if !errors.Is(err, os.ErrExist) && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("restore existing destination error = %v", err)
	}
}

func TestRestoreRejectsArtifactRootUsedByActiveClient(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	root := filepath.Join(dir, "artifacts")
	svc, err := application.OpenPathWithClockAndArtifactRoot(filepath.Join(dir, "source.db"), backupTestClock{}, root)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	created, err := client.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "locked restore"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Ingest(ctx, missis.RequestContext{}, missis.IngestOptions{Target: created.ID, MediaType: "image/png", SourceName: "image.png", Content: strings.NewReader("image")}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	err = client.RestoreWithOptions(ctx, backup, filepath.Join(dir, "restored.db"), missis.RestoreOptions{ArtifactRoot: root})
	if !errors.Is(err, store.ErrMaintenanceBusy) {
		t.Fatalf("restore root busy error = %v, want ErrMaintenanceBusy", err)
	}
}

func mustManifest(t *testing.T, client *missis.Client) missis.ManifestInfo {
	t.Helper()
	manifest, err := client.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

type backupTestClock struct{}

func (backupTestClock) Now() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

func TestVerifyRestoreRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	if _, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "one"}); err != nil {
		t.Fatal(err)
	}
	manifest, err := client.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "two"}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	// The manifest predates the second ticket, so the backup must not match it.
	if err := client.VerifyRestore(ctx, backup, manifest); err == nil {
		t.Fatal("expected verify to reject stale manifest")
	}
}
