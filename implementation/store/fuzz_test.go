package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/implementation/model"
)

// FuzzAppendBatchInvariants explores the append path for states that can break
// the per-stream sequence invariant: a successful append must leave sequences
// contiguous from 1, and a failed append must leave the store fully consistent.
//
// Input layout: (batchLen, op0, seq0, op1, seq1, op2, seq2, op3, seq3).
// batchLen selects 1-4 events; ops select create-entity/create-part/set-value;
// a non-zero seq forces an explicit caller-supplied sequence.
func FuzzAppendBatchInvariants(f *testing.F) {
	f.Add(byte(1), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0)) // one create-entity
	f.Add(byte(2), byte(0), byte(0), byte(1), byte(0), byte(0), byte(0), byte(0), byte(0)) // create-entity + create-part
	f.Add(byte(2), byte(0), byte(0), byte(1), byte(5), byte(0), byte(0), byte(0), byte(0)) // explicit sequence that would open a gap
	f.Add(byte(4), byte(0), byte(0), byte(1), byte(0), byte(2), byte(0), byte(2), byte(0)) // longer mixed batch

	f.Fuzz(func(t *testing.T, batchLen, op0, seq0, op1, seq1, op2, seq2, op3, seq3 byte) {
		n := int(batchLen%4) + 1
		ops := []byte{op0, op1, op2, op3}
		seqs := []byte{seq0, seq1, seq2, seq3}

		tmp := t.TempDir()
		s, err := Open(filepath.Join(tmp, "missis.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		ticketID := model.TicketID("ticket:fuzz")
		stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
		now := time.Now().UTC()
		events := make([]model.Event, 0, n)
		for i := 0; i < n; i++ {
			event := model.Event{
				ID:          model.EventID(fmt.Sprintf("event:%d", i+1)),
				Stream:      stream,
				RecordedAt:  now,
				EffectiveAt: now,
				Actor:       model.ActorRef{Kind: "fuzz", ID: "fuzz"},
			}
			if seqs[i] != 0 {
				event.Sequence = uint64(seqs[i])
			}
			switch ops[i] % 3 {
			case 0:
				event.Operation = model.OpCreateEntity
				event.Target = model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
			case 1:
				path := fmt.Sprintf("p%d", i)
				event.Operation = model.OpCreatePart
				event.Target = model.Ref{Kind: model.KindPart, Entity: "part:" + path, Path: []string{path}}
				event.Value = model.Value{Kind: model.ValueKindText, Text: fmt.Sprintf("v%d", i)}
			case 2:
				path := fmt.Sprintf("p%d", i)
				event.Operation = model.OpSetValue
				event.Target = model.Ref{Kind: model.KindPart, Entity: "part:" + path, Path: []string{path}}
				event.Value = model.Value{Kind: model.ValueKindText, Text: fmt.Sprintf("v%d", i)}
			}
			events = append(events, event)
		}

		_, appendErr := s.AppendBatch(events, "", nil, nil)

		// Whether the append succeeds or fails, the store must be consistent.
		if err := s.CheckConsistency(); err != nil {
			t.Fatalf("store inconsistent after append (append err=%v): %v", appendErr, err)
		}

		if appendErr == nil {
			loaded, err := s.LoadTicketEvents(ticketID)
			if err != nil {
				t.Fatal(err)
			}
			for i, event := range loaded {
				want := uint64(i + 1)
				if event.Sequence != want {
					t.Fatalf("sequence gap after successful append: event %d has sequence %d, want %d (ops=%v seqs=%v)",
						i, event.Sequence, want, ops[:n], seqs[:n])
				}
			}
		}
	})
}
