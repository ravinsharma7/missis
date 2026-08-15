package missis

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/implementation/model"
)

func TestClientOpenHealthBackupAndSequenceGaps(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")
	backup := filepath.Join(tmp, "backup.db")

	client, err := OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	storeID, err := client.StoreID()
	if err != nil {
		t.Fatal(err)
	}
	if storeID == "" {
		t.Fatal("empty store id")
	}
	if err := client.CheckConsistency(); err != nil {
		t.Fatalf("empty store consistency: %v", err)
	}
	if err := client.Backup(backup); err != nil {
		t.Fatal(err)
	}
	gaps, err := client.SequenceGaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Fatalf("empty store has gaps: %+v", gaps)
	}
}

func TestClientAppendListProjection(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	client, err := OpenPath(filepath.Join(tmp, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ticketID := model.TicketID("ticket:test")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "SDK"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, _, err := client.AppendTicketBatch(events, "", nil); err != nil {
		t.Fatal(err)
	}

	summaries, err := client.ListTickets(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(summaries))
	}
	proj, err := client.CurrentProjection(ticketID, now)
	if err != nil {
		t.Fatal(err)
	}
	if proj.TicketID != ticketID {
		t.Fatalf("projection ticket = %q", proj.TicketID)
	}
}
