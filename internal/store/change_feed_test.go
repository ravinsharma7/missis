package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

func TestAcceptedChangeWindowPinsHighWaterDuringConcurrentAppend(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "feed-snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if _, err := s.AppendBatch([]model.Event{changeFeedStoreEvent("event:feed-store:1", "ticket:feed-store:1", at)}, "", nil, nil); err != nil {
		t.Fatal(err)
	}

	var hookErr error
	s.changeReadSnapshotHook = func() {
		s.changeReadSnapshotHook = nil
		_, hookErr = s.AppendBatch([]model.Event{changeFeedStoreEvent("event:feed-store:2", "ticket:feed-store:2", at.Add(time.Second))}, "", nil, nil)
	}
	first, err := s.LoadAcceptedChangeWindowContext(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if hookErr != nil {
		t.Fatal(hookErr)
	}
	if first.HighWater != 1 || len(first.Records) != 1 || first.Records[0].Position != 1 {
		t.Fatalf("first snapshot = %#v", first)
	}
	second, err := s.LoadAcceptedChangeWindowContext(ctx, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if second.HighWater != 2 || len(second.Records) != 1 || second.Records[0].Position != 2 {
		t.Fatalf("second snapshot = %#v", second)
	}
}

func changeFeedStoreEvent(id, ticket string, at time.Time) model.Event {
	stream := model.Ref{Kind: model.KindTicket, Entity: ticket}
	return model.Event{
		ID: model.EventID(id), Stream: stream, Operation: model.OpCreateEntity, Target: stream,
		RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "change-feed"},
	}
}
