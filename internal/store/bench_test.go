package store

import (
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
