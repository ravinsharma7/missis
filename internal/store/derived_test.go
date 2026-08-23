package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

func derivedTicketEvents(t *testing.T, s *Store, id, title string) model.TicketID {
	t.Helper()
	return derivedTicketEventsAt(t, s, id, title, time.Now().UTC())
}

func derivedTicketEventsAt(t *testing.T, s *Store, id, title string, at time.Time) model.TicketID {
	t.Helper()
	ticketID := model.TicketID("ticket:" + id)
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := at
	events := []model.Event{
		{ID: model.EventID("event:" + id + ":1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:" + id + ":2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:" + id + ":title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: title}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:" + id + ":3"), Stream: stream, Sequence: 3, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:" + id + ":status", Path: []string{"status"}}, Value: model.Value{Kind: model.ValueKindStatus, Text: "open"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, _, err := s.AppendTicketBatch(events, "", nil); err != nil {
		t.Fatal(err)
	}
	return ticketID
}

func setStatusEvent(t *testing.T, s *Store, ticketID model.TicketID, id, status string, seq uint64, at time.Time) {
	t.Helper()
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	event := model.Event{
		ID:          model.EventID("event:" + id + ":status"),
		Stream:      stream,
		Sequence:    seq,
		Operation:   model.OpSetValue,
		Target:      model.Ref{Kind: model.KindPart, Entity: "part:" + id + ":status", Path: []string{"status"}},
		Value:       model.Value{Kind: model.ValueKindStatus, Text: status},
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       model.ActorRef{Kind: "test", ID: "test"},
	}
	if _, err := s.AppendBatch([]model.Event{event}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func openDerivedStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDerivedTicketsMaintainedOnAppend(t *testing.T) {
	t.Parallel()
	s := openDerivedStore(t)
	a := derivedTicketEvents(t, s, "a", "Alpha")
	derivedTicketEvents(t, s, "b", "Beta")
	setStatusEvent(t, s, a, "a", "doing", 4, time.Now().UTC())

	summaries, err := s.ListTickets(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d: %+v", len(summaries), summaries)
	}
	byRef := make(map[string]TicketSummary)
	for _, summary := range summaries {
		byRef[summary.Ref] = summary
	}
	if byRef["#1"].Status != "doing" || byRef["#1"].Title != "Alpha" {
		t.Fatalf("ticket #1 = %+v", byRef["#1"])
	}
	if byRef["#2"].Status != "open" || byRef["#2"].Title != "Beta" {
		t.Fatalf("ticket #2 = %+v", byRef["#2"])
	}
	var ticketRows, partRows int
	if err := s.reader.QueryRow(`SELECT COUNT(*) FROM tickets`).Scan(&ticketRows); err != nil {
		t.Fatal(err)
	}
	if ticketRows != 2 {
		t.Fatalf("tickets rows = %d", ticketRows)
	}
	if err := s.reader.QueryRow(`SELECT COUNT(*) FROM parts_current WHERE ticket_id = ?`, string(a)).Scan(&partRows); err != nil {
		t.Fatal(err)
	}
	if partRows != 2 {
		t.Fatalf("parts_current rows for a = %d", partRows)
	}
}

func TestPartsCurrentPersistsOrderKeyAcrossRebuild(t *testing.T) {
	// covers N105
	t.Parallel()
	s := openDerivedStore(t)
	ticketID := model.TicketID("ticket:ordered")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	at := time.Now().UTC()
	orderKey := model.OrderKeyForIndex(7)
	events := []model.Event{
		{ID: "event:ordered:1", Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: stream, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: "event:ordered:2", Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:ordered:body", Path: []string{"body"}}, Value: model.Value{Kind: model.ValueKindMarkdown, Text: "body", OrderKey: orderKey}, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, _, err := s.AppendTicketBatch(events, "", nil); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := s.reader.QueryRow(`SELECT order_key FROM parts_current WHERE ticket_id = ? AND path = 'body'`, string(ticketID)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != orderKey {
		t.Fatalf("stored order key = %q, want %q", stored, orderKey)
	}
	if err := s.RebuildProjection(); err != nil {
		t.Fatal(err)
	}
	if err := s.reader.QueryRow(`SELECT order_key FROM parts_current WHERE ticket_id = ? AND path = 'body'`, string(ticketID)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != orderKey {
		t.Fatalf("rebuilt order key = %q, want %q", stored, orderKey)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRepairsStaleOrderedProjection(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missis.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ticketID := model.TicketID("ticket:stale-order")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	at := time.Now().UTC()
	orderKey := model.OrderKeyForIndex(4)
	events := []model.Event{
		{ID: "event:stale-order:1", Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: stream, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: "event:stale-order:2", Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:stale-order:body", Path: []string{"body"}}, Value: model.Value{Kind: model.ValueKindMarkdown, Text: "body", OrderKey: orderKey}, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, _, err := s.AppendTicketBatch(events, "", nil); err != nil {
		t.Fatal(err)
	}
	var beforeHead string
	if err := s.reader.QueryRow(`SELECT head_hash FROM store_meta WHERE singleton = 1`).Scan(&beforeHead); err != nil {
		t.Fatal(err)
	}
	if _, err := s.writer.Exec(`UPDATE parts_current SET order_key = '' WHERE ticket_id = ? AND path = 'body'`, string(ticketID)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var stored string
	if err := reopened.reader.QueryRow(`SELECT order_key FROM parts_current WHERE ticket_id = ? AND path = 'body'`, string(ticketID)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != orderKey {
		t.Fatalf("repaired order key = %q, want %q", stored, orderKey)
	}
	var afterHead string
	if err := reopened.reader.QueryRow(`SELECT head_hash FROM store_meta WHERE singleton = 1`).Scan(&afterHead); err != nil {
		t.Fatal(err)
	}
	if afterHead != beforeHead {
		t.Fatalf("projection repair changed ledger head: before=%s after=%s", beforeHead, afterHead)
	}
	if err := reopened.CheckConsistency(); err != nil {
		t.Fatalf("consistency after ordered projection repair: %v", err)
	}
}

func TestConcurrentOrderedProjectionRepairAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missis.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	baseID := model.TicketID("ticket:repair-base")
	baseStream := model.Ref{Kind: model.KindTicket, Entity: string(baseID)}
	baseEvents := []model.Event{
		{ID: "event:repair-base:1", Stream: baseStream, Sequence: 1, Operation: model.OpCreateEntity, Target: baseStream, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: "event:repair-base:2", Stream: baseStream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:repair-base:body", Path: []string{"body"}}, Value: model.Value{Kind: model.ValueKindMarkdown, Text: "body", OrderKey: model.OrderKeyForIndex(0)}, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, _, err := s.AppendTicketBatch(baseEvents, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.writer.Exec(`UPDATE parts_current SET order_key = '' WHERE ticket_id = ? AND path = 'body'`, string(baseID)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	const workers = 6
	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			client, openErr := Open(path)
			if openErr != nil {
				errCh <- openErr
				return
			}
			defer client.Close()
			id := model.TicketID(fmt.Sprintf("ticket:repair-worker-%d", i))
			stream := model.Ref{Kind: model.KindTicket, Entity: string(id)}
			events := []model.Event{
				{ID: model.EventID(fmt.Sprintf("event:repair-worker-%d:1", i)), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: stream, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
				{ID: model.EventID(fmt.Sprintf("event:repair-worker-%d:2", i)), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:repair-worker-%d:body", i), Path: []string{"body"}}, Value: model.Value{Kind: model.ValueKindMarkdown, Text: "worker", OrderKey: model.OrderKeyForIndex(0)}, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
			}
			if _, _, appendErr := client.AppendTicketBatch(events, "", nil); appendErr != nil {
				errCh <- appendErr
			}
			if consistencyErr := client.CheckConsistency(); consistencyErr != nil {
				errCh <- consistencyErr
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	final, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Close()
	if err := final.CheckConsistency(); err != nil {
		t.Fatalf("final consistency after concurrent repair/appends: %v", err)
	}
	var repairedKey string
	if err := final.reader.QueryRow(`SELECT order_key FROM parts_current WHERE ticket_id = ? AND path = 'body'`, string(baseID)).Scan(&repairedKey); err != nil {
		t.Fatal(err)
	}
	if repairedKey != model.OrderKeyForIndex(0) {
		t.Fatalf("base order key = %q, want %q", repairedKey, model.OrderKeyForIndex(0))
	}
}

func TestOrderedProjectionRepairKeepsStoresIndependent(t *testing.T) {
	t.Parallel()
	paths := []string{
		filepath.Join(t.TempDir(), "one.db"),
		filepath.Join(t.TempDir(), "two.db"),
	}
	stores := make([]*Store, 0, len(paths))
	for i, path := range paths {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, s)
		id := model.TicketID(fmt.Sprintf("ticket:independent-%d", i))
		stream := model.Ref{Kind: model.KindTicket, Entity: string(id)}
		at := time.Now().UTC()
		events := []model.Event{
			{ID: model.EventID(fmt.Sprintf("event:independent-%d:1", i)), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: stream, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
			{ID: model.EventID(fmt.Sprintf("event:independent-%d:2", i)), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:independent-%d:body", i), Path: []string{"body"}}, Value: model.Value{Kind: model.ValueKindMarkdown, Text: "body", OrderKey: model.OrderKeyForIndex(0)}, RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		}
		if _, _, err := s.AppendTicketBatch(events, "", nil); err != nil {
			t.Fatal(err)
		}
		loaded, err := s.LoadTicketEvents(id)
		if err != nil {
			t.Fatal(err)
		}
		projection, err := model.CurrentProjection(loaded, id, model.MaxRecordedAt(loaded))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := projection.Paths["body"]; !ok {
			t.Fatalf("store %d projection paths = %v", i, projection.Paths)
		}
	}
	defer func() {
		for _, s := range stores {
			_ = s.Close()
		}
	}()
	if _, err := stores[0].writer.Exec(`UPDATE parts_current SET order_key = '' WHERE path = 'body'`); err != nil {
		t.Fatal(err)
	}
	if err := stores[0].Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	stores[0] = reopened
	if err := stores[0].CheckConsistency(); err != nil {
		t.Fatalf("store one after repair: %v", err)
	}
	if err := stores[1].CheckConsistency(); err != nil {
		t.Fatalf("store two while store one repairs: %v", err)
	}
	var repaired string
	if err := stores[0].reader.QueryRow(`SELECT order_key FROM parts_current WHERE ticket_id = ? AND path = 'body'`, "ticket:independent-0").Scan(&repaired); err != nil {
		t.Fatal(err)
	}
	if repaired != model.OrderKeyForIndex(0) {
		t.Fatalf("store one order key = %q", repaired)
	}
}

func TestListTicketsHistoricalFallback(t *testing.T) {
	t.Parallel()
	s := openDerivedStore(t)
	t0 := time.Now().UTC().Add(-2 * time.Hour)
	id := derivedTicketEventsAt(t, s, "h", "Hist", t0)
	setStatusEvent(t, s, id, "h", "doing", 4, t0.Add(time.Hour))

	current, err := s.ListTickets(t0.Add(2 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if current[0].Status != "doing" {
		t.Fatalf("current status = %q", current[0].Status)
	}
	if _, err := s.writer.Exec(`DELETE FROM ticket_aliases`); err != nil {
		t.Fatal(err)
	}
	historical, err := s.ListTickets(t0.Add(30 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if historical[0].Status != "open" {
		t.Fatalf("historical status = %q, want open", historical[0].Status)
	}
	if historical[0].Number != 0 || historical[0].Ref != "#"+shortID(id) {
		t.Fatalf("historical fallback ref = %+v", historical[0])
	}
}

func TestRebuildProjectionRecoversDerived(t *testing.T) {
	t.Parallel()
	s := openDerivedStore(t)
	a := derivedTicketEvents(t, s, "r", "Recover")
	setStatusEvent(t, s, a, "r", "doing", 4, time.Now().UTC())

	if _, err := s.writer.Exec(`DELETE FROM tickets`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.writer.Exec(`DELETE FROM parts_current`); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckConsistency(); err == nil {
		t.Fatal("expected consistency failure after derived corruption")
	}
	if err := s.RebuildProjection(); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("consistency after rebuild: %v", err)
	}
	summaries, err := s.ListTickets(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Status != "doing" {
		t.Fatalf("summaries after rebuild = %+v", summaries)
	}
}

func TestBackfillDerivedOnOpen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missis.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := derivedTicketEvents(t, s, "b", "Backfill")
	setStatusEvent(t, s, a, "b", "doing", 4, time.Now().UTC())
	// Simulate a pre-0004 store: drop derived rows, then reopen.
	if _, err := s.writer.Exec(`DELETE FROM tickets`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.writer.Exec(`DELETE FROM parts_current`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	summaries, err := reopened.ListTickets(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Status != "doing" {
		t.Fatalf("summaries after backfill = %+v", summaries)
	}
	if err := reopened.CheckConsistency(); err != nil {
		t.Fatalf("consistency after backfill: %v", err)
	}
}

// TestOpenHealsDerivedDrift simulates a stale binary (predating migration
// 0004) appending events without maintaining derived rows: after removing one
// derived row and corrupting another ticket's head, reopening must rebuild the
// derived tables so the store is consistent again.
func TestOpenHealsDerivedDrift(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missis.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := derivedTicketEvents(t, s, "a", "Alpha")
	b := derivedTicketEvents(t, s, "b", "Beta")
	if _, err := s.writer.Exec(`DELETE FROM tickets WHERE ticket_id = ?`, string(b)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.writer.Exec(`UPDATE tickets SET head_event = 'stale' WHERE ticket_id = ?`, string(a)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	summaries, err := reopened.ListTickets(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 tickets after open-time heal, got %d", len(summaries))
	}
	if err := reopened.CheckConsistency(); err != nil {
		t.Fatalf("consistency after open-time heal: %v", err)
	}
}

func TestDerivedRetraction(t *testing.T) {
	t.Parallel()
	s := openDerivedStore(t)
	id := derivedTicketEvents(t, s, "x", "Retract")
	now := time.Now().UTC()
	stream := model.Ref{Kind: model.KindTicket, Entity: string(id)}
	target := model.Ref{Kind: model.KindPart, Entity: "part:x:problem", Path: []string{"problem"}}
	set := model.Event{ID: model.EventID("event:x:set"), Stream: stream, Sequence: 4, Operation: model.OpSetValue, Target: target, Value: model.Value{Kind: model.ValueKindText, Text: "body"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}}
	if _, err := s.AppendBatch([]model.Event{set}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	retract := model.Event{ID: model.EventID("event:x:retract"), Stream: stream, Sequence: 5, Operation: model.OpRetractValue, Target: target, RecordedAt: now.Add(time.Second), EffectiveAt: now.Add(time.Second), Actor: model.ActorRef{Kind: "test", ID: "test"}}
	if _, err := s.AppendBatch([]model.Event{retract}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	var valueJSON any
	if err := s.reader.QueryRow(`SELECT value_json FROM parts_current WHERE ticket_id = ? AND path = 'problem'`, string(id)).Scan(&valueJSON); err != nil {
		t.Fatal(err)
	}
	if valueJSON != nil {
		t.Fatalf("expected NULL value for retracted part, got %v", valueJSON)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("consistency after retraction: %v", err)
	}
}

func TestConcurrentAppendsDerivedConsistency(t *testing.T) {
	t.Parallel()
	s := openDerivedStore(t)
	const workers = 8
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			id := model.TicketID(fmt.Sprintf("ticket:conc-%d", w))
			stream := model.Ref{Kind: model.KindTicket, Entity: string(id)}
			now := time.Now().UTC()
			events := []model.Event{
				{ID: model.EventID(fmt.Sprintf("conc-%d-1", w)), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(id)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
				{ID: model.EventID(fmt.Sprintf("conc-%d-2", w)), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:conc-%d-title", w), Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: fmt.Sprintf("title-%d", w)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
				{ID: model.EventID(fmt.Sprintf("conc-%d-3", w)), Stream: stream, Sequence: 3, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:conc-%d-status", w), Path: []string{"status"}}, Value: model.Value{Kind: model.ValueKindStatus, Text: "open"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
			}
			if _, _, err := s.AppendTicketBatch(events, "", nil); err != nil {
				errCh <- err
				return
			}
			set := model.Event{ID: model.EventID(fmt.Sprintf("conc-%d-4", w)), Stream: stream, Sequence: 4, Operation: model.OpSetValue, Target: model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:conc-%d-status", w), Path: []string{"status"}}, Value: model.Value{Kind: model.ValueKindStatus, Text: "doing"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}}
			if _, err := s.AppendBatch([]model.Event{set}, "", nil, nil); err != nil {
				errCh <- err
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("consistency after concurrent appends: %v", err)
	}
	summaries, err := s.ListTickets(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != workers {
		t.Fatalf("expected %d tickets, got %d", workers, len(summaries))
	}
}

func TestDerivedUntouchedByOtherStreamAppend(t *testing.T) {
	t.Parallel()
	s := openDerivedStore(t)
	a := derivedTicketEvents(t, s, "a", "Alpha")
	b := derivedTicketEvents(t, s, "b", "Beta")
	var bHead, bStatus string
	if err := s.reader.QueryRow(`SELECT head_event, status FROM tickets WHERE ticket_id = ?`, string(b)).Scan(&bHead, &bStatus); err != nil {
		t.Fatal(err)
	}
	setStatusEvent(t, s, a, "a", "doing", 4, time.Now().UTC())
	var gotHead, gotStatus string
	if err := s.reader.QueryRow(`SELECT head_event, status FROM tickets WHERE ticket_id = ?`, string(b)).Scan(&gotHead, &gotStatus); err != nil {
		t.Fatal(err)
	}
	if gotHead != bHead || gotStatus != bStatus {
		t.Fatalf("ticket B derived row changed: head %q->%q status %q->%q", bHead, gotHead, bStatus, gotStatus)
	}
}

// TestAppendLoadsOnlyAffectedStream proves append is stream-scoped by
// recording exactly which stream the append path loads.
func TestAppendLoadsOnlyAffectedStream(t *testing.T) {
	t.Parallel()
	s := openDerivedStore(t)
	a := derivedTicketEvents(t, s, "a", "Alpha")
	derivedTicketEvents(t, s, "b", "Beta")
	var loaded []string
	s.appendLoadHook = func(kind, entity string) {
		loaded = append(loaded, kind+":"+entity)
	}
	setStatusEvent(t, s, a, "a", "doing", 4, time.Now().UTC())
	s.appendLoadHook = nil
	summaries, err := s.ListTickets(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].Ref != "#1" || summaries[0].Status != "doing" {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}
	if len(loaded) != 1 || loaded[0] != "ticket:"+string(a) {
		t.Fatalf("append loaded streams %v, want only %s", loaded, a)
	}
}
