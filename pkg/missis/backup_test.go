package missis_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
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
	if err := client.Restore(ctx, backup, restoreDst); err != nil {
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
