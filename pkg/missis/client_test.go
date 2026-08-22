package missis_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestClientOpenHealthBackupAndSequenceGaps(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missis.db")
	backup := filepath.Join(tmp, "backup.db")

	svc, err := application.OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()

	storeID, err := client.StoreID()
	if err != nil {
		t.Fatal(err)
	}
	if storeID == "" {
		t.Fatal("empty store id")
	}
	if err := client.CheckConsistency(context.Background()); err != nil {
		t.Fatalf("empty store consistency: %v", err)
	}
	if err := client.Backup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	gaps, err := client.SequenceGaps(context.Background())
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
	svc, err := application.OpenPath(filepath.Join(tmp, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()

	ticketID := model.TicketID("ticket:test")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	events := []model.Event{
		{ID: model.EventID("event:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "SDK"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
	if _, _, err := client.AppendTicketBatch(context.Background(), events, "", nil); err != nil {
		t.Fatal(err)
	}

	summaries, err := client.ListTickets(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(summaries))
	}
	proj, err := client.CurrentProjection(context.Background(), ticketID, now)
	if err != nil {
		t.Fatal(err)
	}
	if proj.TicketID != ticketID {
		t.Fatalf("projection ticket = %q", proj.TicketID)
	}
}
