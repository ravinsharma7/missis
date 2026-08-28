package eventstoreadapter

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	neutral "github.com/ravinsharma7/skunkwork/packages/eventstore"
)

func TestChangeFeedPagesAcrossStreamsAndResumesAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "feed.db")
	ledger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var feed neutral.ChangeFeed = ledger

	begin, err := feed.BeginChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := feed.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Changes) != 0 || empty.Next != begin || !empty.AtHead {
		t.Fatalf("empty page = %#v", empty)
	}

	at := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	streamA := neutral.Ref{Kind: "run", ID: "run:feed:a"}
	streamB := neutral.Ref{Kind: "run", ID: "run:feed:b"}
	proposal := []neutral.Event{
		spyEvent("event:feed:a1", streamA, "spy.run.started", streamA, `{"n":1}`, at),
		spyEvent("event:feed:b1", streamB, "spy.run.started", streamB, `{"n":2}`, at.Add(time.Second)),
		spyEvent("event:feed:a2", streamA, "spy.probe.observed", streamA, `{"n":3}`, at.Add(2*time.Second)),
		spyEvent("event:feed:b2", streamB, "spy.probe.observed", streamB, `{"n":4}`, at.Add(3*time.Second)),
		spyEvent("event:feed:a3", streamA, "spy.run.completed", streamA, `{"n":5}`, at.Add(4*time.Second)),
	}
	accepted, err := ledger.Append(ctx, neutral.AppendRequest{IdempotencyKey: "feed-across-streams-v1", Events: proposal})
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := eventIDs(accepted.Events)

	first, err := feed.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Changes) != 2 || first.AtHead || first.Next == begin {
		t.Fatalf("first page = %#v", first)
	}
	replayed, err := feed.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replayed, first) {
		t.Fatalf("page redelivery changed\n got: %#v\nwant: %#v", replayed, first)
	}

	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	ledger, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	feed = ledger

	gotIDs := changeIDs(first.Changes)
	next := first.Next
	for !first.AtHead {
		first, err = feed.ReadChanges(ctx, neutral.ReadChangesRequest{After: next, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		gotIDs = append(gotIDs, changeIDs(first.Changes)...)
		next = first.Next
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("feed order = %v, want accepted order %v", gotIDs, wantIDs)
	}
	if next == begin {
		t.Fatal("feed did not advance")
	}
	latest, err := feed.LatestCursor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest != next {
		t.Fatalf("latest cursor differs from traversed head")
	}
	atHead, err := feed.ReadChanges(ctx, neutral.ReadChangesRequest{After: latest, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(atHead.Changes) != 0 || atHead.Next != latest || !atHead.AtHead {
		t.Fatalf("latest page = %#v", atHead)
	}
}

func TestChangeFeedObservesAppendAfterCapturedPage(t *testing.T) {
	ctx := context.Background()
	ledger, err := Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	feed := neutral.ChangeFeed(ledger)
	begin, err := feed.BeginChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream := neutral.Ref{Kind: "run", ID: "run:feed:concurrent"}
	at := time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC)
	if _, err := ledger.Append(ctx, neutral.AppendRequest{IdempotencyKey: "feed-first-v1", Events: []neutral.Event{
		spyEvent("event:feed:first", stream, "spy.run.started", stream, `{}`, at),
	}}); err != nil {
		t.Fatal(err)
	}
	page, err := feed.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !page.AtHead || len(page.Changes) != 1 {
		t.Fatalf("captured page = %#v", page)
	}
	if _, err := ledger.Append(ctx, neutral.AppendRequest{IdempotencyKey: "feed-second-v1", Events: []neutral.Event{
		spyEvent("event:feed:second", stream, "spy.probe.observed", stream, `{}`, at.Add(time.Second)),
	}}); err != nil {
		t.Fatal(err)
	}
	next, err := feed.ReadChanges(ctx, neutral.ReadChangesRequest{After: page.Next, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := changeIDs(next.Changes); !reflect.DeepEqual(got, []string{"event:feed:second"}) || !next.AtHead {
		t.Fatalf("next snapshot = %#v", next)
	}
}

func TestChangeFeedRejectsInvalidAuthorityAndPosition(t *testing.T) {
	ctx := context.Background()
	first, err := Open(filepath.Join(t.TempDir(), "first.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := Open(filepath.Join(t.TempDir(), "second.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	firstFeed := neutral.ChangeFeed(first)
	secondFeed := neutral.ChangeFeed(second)
	firstCursor, err := firstFeed.BeginChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondCursor, err := secondFeed.BeginChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		cursor neutral.ChangeCursor
		limit  uint32
		want   error
	}{
		{name: "empty", cursor: "", limit: 1, want: neutral.ErrCursorInvalid},
		{name: "zero-limit", cursor: firstCursor, limit: 0, want: neutral.ErrChangeLimitInvalid},
		{name: "large-limit", cursor: firstCursor, limit: neutral.MaxChangePageSize + 1, want: neutral.ErrChangeLimitInvalid},
		{name: "foreign", cursor: secondCursor, limit: 1, want: neutral.ErrCursorForeignStore},
		{name: "corrupt", cursor: corruptCursor(firstCursor), limit: 1, want: neutral.ErrCursorCorrupt},
		{name: "unsupported-version", cursor: neutral.ChangeCursor(strings.Replace(string(firstCursor), neutral.ChangeCursorVersionV1, "eventstore-change-cursor-v2", 1)), limit: 1, want: neutral.ErrCursorVersionUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := firstFeed.ReadChanges(ctx, neutral.ReadChangesRequest{After: test.cursor, Limit: test.limit}); !errors.Is(err, test.want) {
				t.Fatalf("ReadChanges error = %v, want %v", err, test.want)
			}
		})
	}

	storeID, err := first.StoreID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := first.store.HeadIntegrityEpochContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	future, err := encodeChangeCursorV1(changeCursorClaimsV1{Version: neutral.ChangeCursorVersionV1, StoreID: storeID, IntegrityEpoch: epoch, Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstFeed.ReadChanges(ctx, neutral.ReadChangesRequest{After: future, Limit: 1}); !errors.Is(err, neutral.ErrCursorFuture) {
		t.Fatalf("future cursor error = %v", err)
	}
	wrongEpoch, err := encodeChangeCursorV1(changeCursorClaimsV1{Version: neutral.ChangeCursorVersionV1, StoreID: storeID, IntegrityEpoch: "future-integrity-epoch-v9", Position: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstFeed.ReadChanges(ctx, neutral.ReadChangesRequest{After: wrongEpoch, Limit: 1}); !errors.Is(err, neutral.ErrCursorEpochMismatch) {
		t.Fatalf("epoch cursor error = %v", err)
	}
	if err := validateChangeCursorWindow(0, 2, 10); !errors.Is(err, neutral.ErrCursorStale) {
		t.Fatalf("stale cursor error = %v", err)
	}
}

func TestChangeFeedCancellationDoesNotAdvance(t *testing.T) {
	ledger, err := Open(filepath.Join(t.TempDir(), "cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	begin, err := ledger.BeginChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page, err := ledger.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if len(page.Changes) != 0 || page.Next != "" {
		t.Fatalf("cancelled call returned usable page %#v", page)
	}
}

func eventIDs(events []neutral.Event) []string {
	ids := make([]string, len(events))
	for index := range events {
		ids[index] = events[index].ID
	}
	return ids
}

func changeIDs(changes []neutral.Change) []string {
	ids := make([]string, len(changes))
	for index := range changes {
		ids[index] = changes[index].Event.ID
		if changes[index].Cursor == "" {
			panic("change has empty cursor")
		}
	}
	return ids
}

func corruptCursor(cursor neutral.ChangeCursor) neutral.ChangeCursor {
	raw := string(cursor)
	last := raw[len(raw)-1]
	if last == '0' {
		last = '1'
	} else {
		last = '0'
	}
	return neutral.ChangeCursor(raw[:len(raw)-1] + string(last))
}
