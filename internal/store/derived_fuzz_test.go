package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

// FuzzDerivedProjectionMatchesLedger appends random set/retract/add sequences
// and asserts after every accepted append that the derived tables equal a
// fold of the stream's authoritative events, and that rejected appends leave
// the store consistent.
func FuzzDerivedProjectionMatchesLedger(f *testing.F) {
	f.Add([]byte{2, 0, 0, 1, 1, 0, 2})       // set p0, retract p0
	f.Add([]byte{3, 0, 2, 1, 1, 2, 1, 2, 2, 2}) // mixed set/retract/add

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 1 || len(data)%3 != 1 {
			return
		}
		n := int(data[0]%10) + 1
		if len(data) < 1+3*n {
			return
		}
		s, err := Open(filepath.Join(t.TempDir(), "missis.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		ctx := context.Background()
		ticketID := model.TicketID("ticket:fuzz-derived")
		stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
		now := time.Now().UTC()
		create := []model.Event{
			{ID: model.EventID("event:fd:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "fuzz", ID: "fuzz"}},
			{ID: model.EventID("event:fd:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:fd:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "fuzz"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "fuzz", ID: "fuzz"}},
			{ID: model.EventID("event:fd:3"), Stream: stream, Sequence: 3, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:fd:status", Path: []string{"status"}}, Value: model.Value{Kind: model.ValueKindStatus, Text: "open"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "fuzz", ID: "fuzz"}},
		}
		if _, _, err := s.AppendTicketBatch(create, "", nil); err != nil {
			t.Fatal(err)
		}
		seq := uint64(4)
		for i := 0; i < n; i++ {
			op := data[1+3*i] % 3
			path := fmt.Sprintf("p%d", data[2+3*i]%6)
			event := model.Event{
				ID:          model.EventID(fmt.Sprintf("event:fd:%d", seq)),
				Stream:      stream,
				Sequence:    seq,
				Target:      model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:fd:%s", path), Path: []string{path}},
				RecordedAt:  now.Add(time.Duration(seq) * time.Second),
				EffectiveAt: now.Add(time.Duration(seq) * time.Second),
				Actor:       model.ActorRef{Kind: "fuzz", ID: "fuzz"},
			}
			switch op {
			case 0:
				event.Operation = model.OpSetValue
				event.Value = model.Value{Kind: model.ValueKindText, Text: fmt.Sprintf("v%d", data[3+3*i]%5)}
			case 1:
				event.Operation = model.OpRetractValue
			case 2:
				event.Operation = model.OpAddValue
				event.Value = model.Value{Kind: model.ValueKindList, Text: fmt.Sprintf("v%d", data[3+3*i]%5)}
			}
			if _, err := s.AppendBatch([]model.Event{event}, "", nil, nil); err != nil {
				if err := s.CheckConsistency(); err != nil {
					t.Fatalf("consistency after rejected append %d: %v", i, err)
				}
				continue
			}
			if err := verifyDerivedForTicket(ctx, s, ticketID); err != nil {
				t.Fatalf("derived mismatch after append %d: %v", i, err)
			}
		}
	})
}

func verifyDerivedForTicket(ctx context.Context, s *Store, ticketID model.TicketID) error {
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	events, err := loadStreamEventsTx(tx, string(model.KindTicket), string(ticketID))
	if err != nil {
		return err
	}
	byStream := map[string][]model.Event{
		string(model.KindTicket) + ":" + string(ticketID): events,
	}
	return verifyDerivedVsLedger(tx, byStream)
}
