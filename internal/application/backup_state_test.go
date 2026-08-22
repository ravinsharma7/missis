package application

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyBackupPublicationStates(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.db")
	svc, err := OpenPath(storePath)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := svc.BackupTo(context.Background(), backup); err != nil {
		_ = svc.Close()
		t.Fatal(err)
	}
	state, err := ClassifyBackup(backup)
	if err != nil || state != BackupStateComplete {
		t.Fatalf("complete state=%q err=%v", state, err)
	}
	if err := os.Remove(backupCompletionPath(backup)); err != nil {
		_ = svc.Close()
		t.Fatal(err)
	}
	state, err = ClassifyBackup(backup)
	if err != nil || state != BackupStateIncomplete {
		t.Fatalf("incomplete state=%q err=%v", state, err)
	}
	if err := os.Remove(backupManifestPath(backup)); err != nil {
		_ = svc.Close()
		t.Fatal(err)
	}
	state, err = ClassifyBackup(backup)
	if err != nil || state != BackupStateIncomplete {
		t.Fatalf("sidecar-only state=%q err=%v", state, err)
	}

	legacy := filepath.Join(dir, "legacy.db")
	if err := svc.Store().Backup(legacy); err != nil {
		_ = svc.Close()
		t.Fatal(err)
	}
	state, err = ClassifyBackup(legacy)
	if err != nil || state != BackupStateLegacyV1 {
		t.Fatalf("legacy state=%q err=%v", state, err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
}
