package benchmark

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
)

// BenchmarkListTickets measures how the TUI's refresh path (ListTickets) grows
// with store size. Each ticket carries 5 events, so the event ledger is
// 5x the ticket count. This supports the projection-snapshot decision (#51);
// append is stream-scoped now, so seeding is no longer quadratic (#61).
func BenchmarkListTickets(b *testing.B) {
	for _, tickets := range []int{10, 50, 200, 400} {
		b.Run(fmt.Sprintf("tickets=%d", tickets), func(b *testing.B) {
			tmp := b.TempDir()
			s, err := store.Open(filepath.Join(tmp, "missis.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			now := time.Now().UTC()
			for i := 0; i < tickets; i++ {
				if _, _, err := s.AppendTicketBatch(benchTicketEvents(i, now), "", nil); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.ListTickets(now); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

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
	create := model.Event{
		ID:          model.EventID(fmt.Sprintf("bench-%d-0", i)),
		Stream:      stream,
		Operation:   model.OpCreateEntity,
		Target:      model.Ref{Kind: model.KindTicket, Entity: string(ticketID)},
		RecordedAt:  now,
		EffectiveAt: now,
		Actor:       model.ActorRef{Kind: "bench", ID: "bench"},
	}
	return []model.Event{
		create,
		part(1, "title", model.Value{Kind: model.ValueKindText, Text: "benchmark ticket"}),
		part(2, "status", model.Value{Kind: model.ValueKindStatus, Text: "open"}),
		part(3, "priority", model.Value{Kind: model.ValueKindPriority, Text: "medium"}),
		part(4, "type", model.Value{Kind: model.ValueKindList, List: []string{"bench"}}),
	}
}
