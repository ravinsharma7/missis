package store

import (
	"path/filepath"
	"testing"
)

func TestOpenCloseAndBackup(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")
	backup := filepath.Join(tmp, "backup.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := s.Backup(backup); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close after backup: %v", err)
	}
	backupStore, err := Open(backup)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	if err := backupStore.Close(); err != nil {
		t.Fatalf("close backup: %v", err)
	}
}
