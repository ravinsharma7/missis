package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestOpenCreatesPrivateStore(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	storeDir := filepath.Join(tmp, "store")
	path := filepath.Join(storeDir, "missis.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("store dir mode = %04o, want 0700", perm)
	}
	fi, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("store file mode = %04o, want 0600", perm)
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

func TestOpenDetectsTamperedEvent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ticketID := model.TicketID("ticket:tamper")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "Original"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, err := s.AppendBatch(events, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	var raw string
	if err := rawDB.QueryRow(`SELECT event_json FROM events WHERE id = 'event:2'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "Original") {
		t.Fatalf("expected Original in event json: %s", raw)
	}
	tampered := strings.Replace(raw, "Original", "Tampered", 1)
	if _, err := rawDB.Exec(`UPDATE events SET event_json = ? WHERE id = 'event:2'`, tampered); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil {
		t.Fatal("expected Open to fail after event tampering")
	}
}

func TestCheckConsistencyDetectsTamperedHashRow(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ticketID := model.TicketID("ticket:hashrow")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "Row"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, err := s.AppendBatch(events, "", nil, nil); err != nil {
		t.Fatal(err)
	}

	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.Exec(`UPDATE event_hashes SET hash = 'deadbeef' WHERE event_id = 'event:2'`); err != nil {
		t.Fatal(err)
	}

	if err := s.CheckConsistency(); err == nil {
		t.Fatal("expected consistency failure after hash row tampering")
	}
}

func TestCheckConsistencyDetectsColumnPayloadMismatch(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ticketID := model.TicketID("ticket:column")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "Column"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, err := s.AppendBatch(events, "", nil, nil); err != nil {
		t.Fatal(err)
	}

	// Tamper only the denormalized sequence column, leaving the authoritative
	// event_json payload untouched. The payload must remain the source of
	// truth, so the mismatch must be reported.
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.Exec(`UPDATE events SET sequence = 99 WHERE id = 'event:2'`); err != nil {
		t.Fatal(err)
	}

	if err := s.CheckConsistency(); err == nil {
		t.Fatal("expected consistency failure after column/payload mismatch")
	}
}

func TestSequenceGapIsIncidentNotAutoRepaired(t *testing.T) {
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

	// Simulate the historical allocation bug: next_sequence skips a number
	// without any event being written for it. The hash chain stays valid
	// because it is built over the events actually accepted.
	tx, err := s.writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE streams SET next_sequence = 3 WHERE stream_kind = ? AND stream_entity = ?`, string(model.KindTicket), string(ticketID)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// The next append crosses the skipped number: events 1, 2, 4.
	if _, err := s.AppendBatch([]model.Event{
		{ID: model.EventID("event:3"), Stream: stream, Operation: model.OpSetValue, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "after-gap"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}, "", nil, nil); err != nil {
		t.Fatalf("append across skipped sequence: %v", err)
	}

	// The gap is detected and reported, not hidden.
	gaps, err := s.SequenceGaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || len(gaps[0].Missing) != 1 || gaps[0].Missing[0] != 3 {
		t.Fatalf("unexpected gaps: %+v", gaps)
	}
	// In-place repair refuses to rewrite accepted events.
	if err := s.RepairSequenceGaps(); err == nil {
		t.Fatal("expected RepairSequenceGaps to refuse in-place repair")
	}

	// The store remains otherwise consistent: sequences are unique and
	// strictly increasing (1, 2, 4), and no event bytes were rewritten.
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("consistency with gap: %v", err)
	}
	loaded, err := s.LoadTicketEvents(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 || loaded[0].Sequence != 1 || loaded[1].Sequence != 2 || loaded[2].Sequence != 4 {
		t.Fatalf("event bytes must not be rewritten: %+v", loaded)
	}

	// The gap remains visible; nothing erased it.
	gaps, err = s.SequenceGaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || gaps[0].Missing[0] != 3 {
		t.Fatalf("gap must remain visible: %+v", gaps)
	}
}

func TestAppendRetryFaultInjection(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	s, err := Open(filepath.Join(tmp, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	failures := 2
	attempts := 0
	s.appendCommitHook = func() error {
		attempts++
		if failures > 0 {
			failures--
			return fmt.Errorf("database is locked")
		}
		return nil
	}

	ticketID := model.TicketID("ticket:retry")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:r1"), Stream: stream, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:r2"), Stream: stream, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "retry"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	outcome, err := s.AppendBatch(events, "", nil, nil)
	if err != nil {
		t.Fatalf("append with retryable commit failures: %v", err)
	}
	if outcome.Replayed {
		t.Fatal("append was replayed unexpectedly")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 commit attempts (2 failures + 1 success), got %d", attempts)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("consistency after retried append: %v", err)
	}
	loaded, err := s.LoadTicketEvents(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 events, got %d", len(loaded))
	}
	for i, event := range loaded {
		want := uint64(i + 1)
		if event.Sequence != want {
			t.Fatalf("event %d has sequence %d, want %d", i, event.Sequence, want)
		}
	}

	// A subsequent append must continue the same contiguous stream.
	s.appendCommitHook = nil
	if _, err := s.AppendBatch([]model.Event{
		{ID: model.EventID("event:r3"), Stream: stream, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:p2", Path: []string{"p2"}}, Value: model.Value{Kind: model.ValueKindText, Text: "next"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}, "", nil, nil); err != nil {
		t.Fatalf("subsequent append: %v", err)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("consistency after subsequent append: %v", err)
	}
	loaded, err = s.LoadTicketEvents(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 events, got %d", len(loaded))
	}
	for i, event := range loaded {
		want := uint64(i + 1)
		if event.Sequence != want {
			t.Fatalf("event %d has sequence %d, want %d", i, event.Sequence, want)
		}
	}
}

func TestConcurrentAppendNeverCreatesGaps(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")

	appender, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer appender.Close()

	now := time.Now().UTC()
	const tickets = 3
	const seedEvents = 3
	for ticket := 0; ticket < tickets; ticket++ {
		ticketID := model.TicketID(fmt.Sprintf("ticket:stress-%d", ticket))
		stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
		events := []model.Event{
			{ID: model.EventID(fmt.Sprintf("event:s%dt0", ticket)), Stream: stream, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
			{ID: model.EventID(fmt.Sprintf("event:s%dt1", ticket)), Stream: stream, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "stress"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
			{ID: model.EventID(fmt.Sprintf("event:s%dt2", ticket)), Stream: stream, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:status", Path: []string{"status"}}, Value: model.Value{Kind: model.ValueKindStatus, Text: "open"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		}
		if _, err := appender.AppendBatch(events, "", nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	const iterations = 30
	start := make(chan struct{})
	var wg sync.WaitGroup
	appendErrs := make(chan error, iterations*3)

	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				ticket := i % tickets
				ticketID := model.TicketID(fmt.Sprintf("ticket:stress-%d", ticket))
				stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
				part := fmt.Sprintf("part:p%d-%d", w, i)
				if _, err := appender.AppendBatch([]model.Event{
					{ID: model.EventID(fmt.Sprintf("event:a%d-%d", w, i)), Stream: stream, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: part, Path: []string{fmt.Sprintf("p%d-%d", w, i)}}, Value: model.Value{Kind: model.ValueKindText, Text: "x"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
				}, "", nil, nil); err != nil {
					appendErrs <- err
					return
				}
			}
		}(w)
	}
	close(start)
	wg.Wait()
	close(appendErrs)
	for err := range appendErrs {
		t.Fatalf("concurrent append failed: %v", err)
	}

	if err := appender.CheckConsistency(); err != nil {
		t.Fatalf("consistency after concurrent append stress: %v", err)
	}
	gaps, err := appender.SequenceGaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Fatalf("concurrent appends created sequence gaps: %+v", gaps)
	}

	// Every stream must still accept appends after the storm without opening
	// a hole.
	for ticket := 0; ticket < tickets; ticket++ {
		ticketID := model.TicketID(fmt.Sprintf("ticket:stress-%d", ticket))
		stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
		if _, err := appender.AppendBatch([]model.Event{
			{ID: model.EventID(fmt.Sprintf("event:after-%d", ticket)), Stream: stream, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:after%d", ticket), Path: []string{fmt.Sprintf("after%d", ticket)}}, Value: model.Value{Kind: model.ValueKindText, Text: "y"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		}, "", nil, nil); err != nil {
			t.Fatalf("append after stress (ticket %d): %v", ticket, err)
		}
	}
	gaps, err = appender.SequenceGaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Fatalf("sequence gaps after post-storm appends: %+v", gaps)
	}
}

func TestOpenChurnConsistency(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")

	s0, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	const tickets = 4
	for ticket := 0; ticket < tickets; ticket++ {
		ticketID := model.TicketID(fmt.Sprintf("ticket:churn-%d", ticket))
		stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
		events := []model.Event{
			{ID: model.EventID(fmt.Sprintf("event:c%dt0", ticket)), Stream: stream, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
			{ID: model.EventID(fmt.Sprintf("event:c%dt1", ticket)), Stream: stream, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "churn"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		}
		if _, err := s0.AppendBatch(events, "", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s0.Close(); err != nil {
		t.Fatal(err)
	}

	const iterations = 60
	var wg sync.WaitGroup
	churnErrs := make(chan error, iterations)
	appendErrs := make(chan error, iterations)

	// Open/Close churn is what every client process does; each Open rebuilds
	// the hash chain. Concurrent with appends, that used to leave stale heads
	// and spurious "head hash mismatch" failures.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s, err := Open(path)
			if err != nil {
				churnErrs <- fmt.Errorf("open churn %d: %w", i, err)
				return
			}
			if err := s.CheckConsistency(); err != nil {
				churnErrs <- fmt.Errorf("consistency during open churn %d: %w", i, err)
				s.Close()
				return
			}
			if err := s.Close(); err != nil {
				churnErrs <- err
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s, err := Open(path)
		if err != nil {
			appendErrs <- err
			return
		}
		defer s.Close()
		for i := 0; i < iterations; i++ {
			ticket := i % tickets
			ticketID := model.TicketID(fmt.Sprintf("ticket:churn-%d", ticket))
			stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
			if _, err := s.AppendBatch([]model.Event{
				{ID: model.EventID(fmt.Sprintf("event:churn-a%d", i)), Stream: stream, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:a%d", i), Path: []string{fmt.Sprintf("a%d", i)}}, Value: model.Value{Kind: model.ValueKindText, Text: "x"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
			}, "", nil, nil); err != nil {
				appendErrs <- fmt.Errorf("append during churn %d: %w", i, err)
				return
			}
		}
	}()

	wg.Wait()
	close(churnErrs)
	close(appendErrs)
	for err := range churnErrs {
		t.Fatal(err)
	}
	for err := range appendErrs {
		t.Fatal(err)
	}

	final, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Close()
	if err := final.CheckConsistency(); err != nil {
		t.Fatalf("final consistency after open churn: %v", err)
	}
	gaps, err := final.SequenceGaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Fatalf("gaps after open churn: %+v", gaps)
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
