package store

import (
	"encoding/json"
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
	originalID, err := s.StoreID()
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
	restoredID, _ := restored.StoreID()
	if restoredID != originalID {
		t.Fatalf("store id changed after restore: %s != %s", restoredID, originalID)
	}
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

func TestStoreIdentityAndHeadHash(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	s, err := Open(filepath.Join(tmp, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	storeID, err := s.StoreID()
	if err != nil {
		t.Fatal(err)
	}
	if storeID == "" {
		t.Fatal("store id is empty")
	}
	head, err := s.HeadHash()
	if err != nil {
		t.Fatal(err)
	}
	if head != "" {
		t.Fatalf("empty store should have empty head, got %s", head)
	}

	ticketID := model.TicketID("ticket:test")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, err := s.AppendBatch(events, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	head, err = s.HeadHash()
	if err != nil {
		t.Fatal(err)
	}
	if head == "" {
		t.Fatal("head hash is empty after append")
	}
}

func TestDivergingHeads(t *testing.T) {
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
	if _, err := s.AppendBatch([]model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	original, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	if _, err := original.AppendBatch([]model.Event{
		{ID: model.EventID("event:2"), Stream: stream, Sequence: 2, Operation: model.OpSetValue, Target: model.Ref{Kind: model.KindPart, Entity: "part:p", Path: []string{"p"}}, Value: model.Value{Kind: model.ValueKindText, Text: "x"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	originalHead, _ := original.HeadHash()
	restoredHead, _ := restored.HeadHash()
	if originalHead == restoredHead {
		t.Fatalf("expected divergent heads, both are %s", originalHead)
	}
}

func BenchmarkEventHash(b *testing.B) {
	event := model.Event{
		ID:        model.EventID("event:bench"),
		Sequence:  1,
		Operation: model.OpSetValue,
		Target:    model.Ref{Kind: model.KindPart, Entity: "part:p", Path: []string{"p"}},
		Value:     model.Value{Kind: model.ValueKindText, Text: "bench"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = computeEventHash(event, "previous")
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

func TestRepairSequenceGaps(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	s, err := Open(filepath.Join(tmp, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ticketID := model.TicketID("ticket:gap")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "gap"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, err := s.AppendBatch(events, "", nil, nil); err != nil {
		t.Fatal(err)
	}

	events[1].Sequence = 3
	raw, err := json.Marshal(events[1])
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE events SET sequence = 3, event_json = ? WHERE id = ?`, string(raw), "event:2"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE streams SET next_sequence = 4 WHERE stream_kind = ? AND stream_entity = ?`, string(model.KindTicket), string(ticketID)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	gaps, err := s.SequenceGaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || len(gaps[0].Missing) != 1 || gaps[0].Missing[0] != 2 {
		t.Fatalf("unexpected gaps: %+v", gaps)
	}
	if err := s.RepairSequenceGaps(); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("consistency after repair: %v", err)
	}
	repaired, err := s.LoadTicketEvents(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if len(repaired) != 2 || repaired[1].Sequence != 2 {
		t.Fatalf("unexpected repaired events: %+v", repaired)
	}
}

func TestJournalModeWAL(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	s, err := Open(filepath.Join(tmp, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mode string
	if err := s.reader.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal mode = %q, want wal", mode)
	}
}

func TestLoadLinkEventsEquivalence(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	s, err := Open(filepath.Join(tmp, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	from := model.TicketID("ticket:from")
	to := model.TicketID("ticket:to")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(from)}
	now := time.Now().UTC()
	toRef := model.Ref{Kind: model.KindTicket, Entity: string(to)}
	events := []model.Event{
		{ID: model.EventID("event:create"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(from)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:link"), Stream: stream, Sequence: 2, Operation: model.OpAssertLink, Target: model.Ref{Kind: model.KindTicket, Entity: string(from)}, Value: model.Value{Text: "blocked-by", Ref: &toRef}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, err := s.AppendBatch(events, "", nil, nil); err != nil {
		t.Fatal(err)
	}

	linkEvents, err := s.LoadLinkEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(linkEvents) != 1 || linkEvents[0].Operation != model.OpAssertLink {
		t.Fatalf("expected one link event, got %+v", linkEvents)
	}

	allEvents, err := s.LoadEvents()
	if err != nil {
		t.Fatal(err)
	}
	start := model.Ref{Kind: model.KindTicket, Entity: string(from)}
	allGraph, err := model.BuildLineageGraph(allEvents, now.Add(time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	linkGraph, err := model.BuildLineageGraph(linkEvents, now.Add(time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	allEdges, _ := allGraph.Walk(start, "both", 3, nil)
	linkEdges, _ := linkGraph.Walk(start, "both", 3, nil)
	if len(allEdges) != len(linkEdges) {
		t.Fatalf("lineage mismatch: all=%d link=%d", len(allEdges), len(linkEdges))
	}
}
