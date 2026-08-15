package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/implementation/model"
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

func TestBackupRestore(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")
	backup := filepath.Join(tmp, "backup.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ticketID := model.TicketID("ticket:test")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "Backup"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, err := s.AppendBatch(events, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	loaded, err := restored.LoadTicketEvents(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 events after restore, got %d", len(loaded))
	}
	if err := restored.CheckConsistency(); err != nil {
		t.Fatalf("consistency after restore: %v", err)
	}
}

func TestCheckConsistencyHealthy(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	s, err := Open(filepath.Join(tmp, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("empty store should be consistent: %v", err)
	}
}
