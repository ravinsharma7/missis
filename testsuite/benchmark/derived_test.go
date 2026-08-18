package benchmark

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
)

// BenchmarkListTicketsLarge measures ListTickets against a populated derived
// table. It should scale with ticket count, not total events (#51).
func BenchmarkListTicketsLarge(b *testing.B) {
	for _, tickets := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("tickets=%d", tickets), func(b *testing.B) {
			s, err := store.Open(filepath.Join(b.TempDir(), "missis.db"))
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

// BenchmarkAppendWithUnrelatedStream measures append cost while another stream
// holds many events. It must stay flat as the unrelated stream grows; that is
// the proof that append is stream-scoped (#51/#61).
func BenchmarkAppendWithUnrelatedStream(b *testing.B) {
	for _, parts := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("unrelated=%d", parts), func(b *testing.B) {
			s, err := store.Open(filepath.Join(b.TempDir(), "missis.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			now := time.Now().UTC()

			bigID := model.TicketID("ticket:big")
			bigStream := model.Ref{Kind: model.KindTicket, Entity: string(bigID)}
			bigEvents := []model.Event{
				{ID: model.EventID("big:0"), Stream: bigStream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(bigID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "bench", ID: "bench"}},
			}
			for j := 0; j < parts; j++ {
				bigEvents = append(bigEvents, model.Event{
					ID:          model.EventID(fmt.Sprintf("big:%d", j+1)),
					Stream:      bigStream,
					Sequence:    uint64(j + 2),
					Operation:   model.OpCreatePart,
					Target:      model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:big:%d", j), Path: []string{fmt.Sprintf("p%d", j)}},
					Value:       model.Value{Kind: model.ValueKindText, Text: "x"},
					RecordedAt:  now,
					EffectiveAt: now,
					Actor:       model.ActorRef{Kind: "bench", ID: "bench"},
				})
			}
			if _, _, err := s.AppendTicketBatch(bigEvents, "", nil); err != nil {
				b.Fatal(err)
			}

			smallID := model.TicketID("ticket:small")
			smallStream := model.Ref{Kind: model.KindTicket, Entity: string(smallID)}
			smallCreate := []model.Event{
				{ID: model.EventID("small:0"), Stream: smallStream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(smallID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "bench", ID: "bench"}},
				{ID: model.EventID("small:1"), Stream: smallStream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:small:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "small"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "bench", ID: "bench"}},
				{ID: model.EventID("small:2"), Stream: smallStream, Sequence: 3, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:small:status", Path: []string{"status"}}, Value: model.Value{Kind: model.ValueKindStatus, Text: "open"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "bench", ID: "bench"}},
			}
			if _, _, err := s.AppendTicketBatch(smallCreate, "", nil); err != nil {
				b.Fatal(err)
			}

			seq := uint64(4)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				event := model.Event{
					ID:          model.EventID(fmt.Sprintf("small:%d", seq)),
					Stream:      smallStream,
					Sequence:    seq,
					Operation:   model.OpSetValue,
					Target:      model.Ref{Kind: model.KindPart, Entity: "part:small:status", Path: []string{"status"}},
					Value:       model.Value{Kind: model.ValueKindStatus, Text: "doing"},
					RecordedAt:  now,
					EffectiveAt: now,
					Actor:       model.ActorRef{Kind: "bench", ID: "bench"},
				}
				if _, err := s.AppendBatch([]model.Event{event}, "", nil, nil); err != nil {
					b.Fatal(err)
				}
				seq++
			}
		})
	}
}
