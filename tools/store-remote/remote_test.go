package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestLocalRemoteUploadSkipForceAndVerify(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	svc, err := application.OpenPath(filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	created, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValue{Target: created.Ref + "/problem", Value: "body"}); err != nil {
		t.Fatal(err)
	}
	manifest, err := client.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	remote := &localRemote{dir: filepath.Join(dir, "remote")}
	key, err := uploadBackup(ctx, remote, manifest, backup, false)
	if err != nil {
		t.Fatal(err)
	}
	wantKey := manifest.StoreID + "/" + manifest.HeadHash + ".db"
	if key != wantKey {
		t.Fatalf("key = %q, want %q", key, wantKey)
	}
	exists, err := remote.Exists(ctx, key)
	if err != nil || !exists {
		t.Fatalf("uploaded key not present: exists=%v err=%v", exists, err)
	}

	if _, err := uploadBackup(ctx, remote, manifest, backup, false); err == nil {
		t.Fatal("expected skip on existing backup without force")
	}
	if _, err := uploadBackup(ctx, remote, manifest, backup, true); err != nil {
		t.Fatalf("forced upload: %v", err)
	}

	dst := filepath.Join(dir, "downloaded.db")
	if err := downloadAndVerify(ctx, remote, manifest, dst); err != nil {
		t.Fatalf("download verify: %v", err)
	}
	restoredSvc, err := application.OpenPath(dst)
	if err != nil {
		t.Fatal(err)
	}
	restored := missis.NewClient(restoredSvc)
	proj, err := restored.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	restored.Close()
	if err != nil {
		t.Fatal(err)
	}
	if proj.Parts["problem"].Value != "body" {
		t.Fatalf("downloaded projection = %+v", proj)
	}
}

func TestDownloadVerifyRejectsTamperedBackup(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, err := application.OpenPath(filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	if _, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "tamper"}); err != nil {
		t.Fatal(err)
	}
	manifest, err := client.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := client.BackupTo(ctx, backup); err != nil {
		t.Fatal(err)
	}
	client.Close()

	remote := &localRemote{dir: filepath.Join(dir, "remote")}
	key, err := uploadBackup(ctx, remote, manifest, backup, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remote.keyPath(key), []byte("not a sqlite store"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadAndVerify(ctx, remote, manifest, filepath.Join(dir, "bad.db")); err == nil {
		t.Fatal("expected verification to reject tampered backup")
	}
}

func TestResolveRemoteRequiresConfig(t *testing.T) {
	keys := []string{"MISSIS_REMOTE_DIR", "MISSIS_RCLONE_REMOTE", "MISSIS_S3_BUCKET"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	if _, err := resolveRemote(); err == nil {
		t.Fatal("expected error without any remote config")
	}
	t.Setenv("MISSIS_REMOTE_DIR", t.TempDir())
	r, err := resolveRemote()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.(*localRemote); !ok {
		t.Fatalf("expected localRemote, got %T", r)
	}
}

func TestDefaultBackupPath(t *testing.T) {
	path := defaultBackupPath(missis.ManifestInfo{StoreID: "store:01ABC", HeadHash: "hash123"})
	if path != filepath.Join("backups", "store_01ABC-hash123.db") {
		t.Fatalf("path = %q", path)
	}
	if !strings.Contains(path, "-") {
		t.Fatalf("path missing separator: %q", path)
	}
}
