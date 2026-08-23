package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

func benchTicketEvents(i int, now time.Time) []model.Event {
	ticketID := model.TicketID(fmt.Sprintf("ticket:bench-%d", i))
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	part := func(j int, path string, value model.Value) model.Event {
		return model.Event{
			ID:          model.EventID(fmt.Sprintf("bench-%d-%d", i, j)),
			Stream:      stream,
			Operation:   model.OpCreatePart,
			Target:      model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:bench-%d-%d", i, j), Path: []string{path}},
			Value:       value,
			RecordedAt:  now,
			EffectiveAt: now,
			Actor:       model.ActorRef{Kind: "bench", ID: "bench"},
		}
	}
	return []model.Event{
		{ID: model.EventID(fmt.Sprintf("bench-%d-0", i)), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "bench", ID: "bench"}},
		part(1, "title", model.Value{Kind: model.ValueKindText, Text: "benchmark ticket"}),
		part(2, "status", model.Value{Kind: model.ValueKindStatus, Text: "open"}),
		part(3, "priority", model.Value{Kind: model.ValueKindPriority, Text: "medium"}),
		part(4, "type", model.Value{Kind: model.ValueKindList, List: []string{"bench"}}),
	}
}

func seedBenchStore(b *testing.B, tickets int) *Store {
	b.Helper()
	s, err := Open(filepath.Join(b.TempDir(), "missis.db"))
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < tickets; i++ {
		if _, _, err := s.AppendTicketBatch(benchTicketEvents(i, now), "", nil); err != nil {
			b.Fatal(err)
		}
	}
	return s
}

func seedOrderedBenchStore(b *testing.B, eventCount int) *Store {
	b.Helper()
	// Seed a valid ledger/projection snapshot directly so the 100,000-event
	// setup does not spend minutes exercising the public append path. The
	// event hash chain is still populated, so the measured store state has the
	// same integrity shape as a production store.
	s, err := Open(filepath.Join(b.TempDir(), "ordered.db"))
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	ticketID := model.TicketID("ticket:bench-ordered")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	events := make([]model.Event, 0, eventCount)
	events = append(events, model.Event{
		ID:          "event:bench-ordered:0",
		Stream:      stream,
		Sequence:    1,
		Operation:   model.OpCreateEntity,
		Target:      stream,
		RecordedAt:  now,
		EffectiveAt: now,
		Actor:       model.ActorRef{Kind: "bench", ID: "bench"},
	})
	for i := 1; i < eventCount; i++ {
		path := fmt.Sprintf("part-%06d", i)
		events = append(events, model.Event{
			ID:          model.EventID(fmt.Sprintf("event:bench-ordered:%d", i)),
			Stream:      stream,
			Sequence:    uint64(i + 1),
			Operation:   model.OpCreatePart,
			Target:      model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:bench-ordered:%d", i), Path: []string{path}},
			Value:       model.Value{Kind: model.ValueKindMarkdown, Text: path, OrderKey: model.OrderKeyForIndex(i - 1)},
			RecordedAt:  now,
			EffectiveAt: now,
			Actor:       model.ActorRef{Kind: "bench", ID: "bench"},
		})
	}
	tx, err := s.writer.Begin()
	if err != nil {
		b.Fatal(err)
	}
	previousHash := ""
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO events (id, stream_kind, stream_entity, sequence, event_json) VALUES (?, ?, ?, ?, ?)`, event.ID, event.Stream.Kind, event.Stream.Entity, event.Sequence, raw); err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		hash := computeEventHash(event, previousHash)
		if _, err := tx.Exec(`INSERT INTO event_hashes (event_id, previous_hash, hash) VALUES (?, ?, ?)`, event.ID, previousHash, hash); err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		previousHash = hash
	}
	if _, err := tx.Exec(`UPDATE store_meta SET head_hash = ? WHERE singleton = 1`, previousHash); err != nil {
		tx.Rollback()
		b.Fatal(err)
	}
	head := events[len(events)-1]
	if _, err := tx.Exec(`INSERT INTO tickets (ticket_id, alias, title, status, head_event, recorded_at) VALUES (?, ?, ?, ?, ?, ?)`, string(ticketID), 0, "benchmark ticket", "open", string(head.ID), now.Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		b.Fatal(err)
	}
	for i := 1; i < len(events); i++ {
		event := events[i]
		path := event.Target.Path[0]
		valueJSON, err := json.Marshal(event.Value)
		if err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO parts_current (ticket_id, path, part_id, value_json, value_kind, parent_id, created_by, current_event, order_key) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, string(ticketID), path, event.Target.Entity, string(valueJSON), string(event.Value.Kind), nil, event.ID, event.ID, event.Value.OrderKey); err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return s
}

// BenchmarkEnsureDerivedFresh isolates the open-time derived-freshness check
// (count queries + per-ticket head comparison) from hash-chain verification,
// at increasing ledger sizes.
func BenchmarkEnsureDerivedFresh(b *testing.B) {
	for _, tickets := range []int{200, 1000, 5000} {
		b.Run(fmt.Sprintf("tickets=%d", tickets), func(b *testing.B) {
			s := seedBenchStore(b, tickets)
			defer s.Close()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := ensureDerivedFresh(s.reader); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStoreOpen measures full open cost (hash verification + derived
// freshness check) so the added check can be weighed against the ledger size.
func BenchmarkStoreOpen(b *testing.B) {
	for _, tickets := range []int{200, 1000, 5000} {
		b.Run(fmt.Sprintf("tickets=%d", tickets), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "missis.db")
			s, err := Open(path)
			if err != nil {
				b.Fatal(err)
			}
			now := time.Now().UTC()
			for i := 0; i < tickets; i++ {
				if _, _, err := s.AppendTicketBatch(benchTicketEvents(i, now), "", nil); err != nil {
					b.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s, err := Open(path)
				if err != nil {
					b.Fatal(err)
				}
				if err := s.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkOrderedProjectionRepair measures the order-key candidate scan,
// repair path, and full rebuild at the requested ledger sizes. Setup is not
// included in the measured operation.
func BenchmarkOrderedProjectionRepair(b *testing.B) {
	for _, events := range []int{100, 1000, 10000, 100000} {
		b.Run(fmt.Sprintf("events=%d/healthy", events), func(b *testing.B) {
			s := seedOrderedBenchStore(b, events)
			defer s.Close()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := ensureDerivedFresh(s.writer); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("events=%d/drift-rebuild", events), func(b *testing.B) {
			s := seedOrderedBenchStore(b, events)
			defer s.Close()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				if _, err := s.writer.Exec(`UPDATE parts_current SET order_key = '' WHERE ticket_id = ? AND path = ?`, "ticket:bench-ordered", "part-000001"); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if err := ensureDerivedFresh(s.writer); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("events=%d/rebuild", events), func(b *testing.B) {
			s := seedOrderedBenchStore(b, events)
			defer s.Close()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := rebuildDerived(s.writer); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkConcurrentOrderedProjectionRefresh(b *testing.B) {
	s := seedOrderedBenchStore(b, 10000)
	defer s.Close()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := ensureDerivedFresh(s.writer); err != nil {
				b.Fatal(err)
			}
		}
	})
}
