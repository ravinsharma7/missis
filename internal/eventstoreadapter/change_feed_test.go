package eventstoreadapter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
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
	if err := validateChangeCursorWindow(0, 2, 10, true); !errors.Is(err, neutral.ErrCursorStale) {
		t.Fatalf("stale cursor error = %v", err)
	}
	if err := validateChangeCursorWindow(1, 2, 10, false); !errors.Is(err, neutral.ErrChangeFeedIntegrity) {
		t.Fatalf("undeclared retention gap error = %v", err)
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

func TestChangeFeedCursorResumesVerifiedBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.db")
	backup := filepath.Join(dir, "backup.db")
	ledger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := ledger.BeginChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream := neutral.Ref{Kind: "run", ID: "run:feed:backup"}
	at := time.Date(2026, 8, 28, 13, 45, 0, 0, time.UTC)
	if _, err := ledger.Append(ctx, neutral.AppendRequest{IdempotencyKey: "feed-backup-v1", Events: []neutral.Event{
		spyEvent("event:feed:backup:1", stream, "spy.run.started", stream, `{}`, at),
		spyEvent("event:feed:backup:2", stream, "spy.run.completed", stream, `{}`, at.Add(time.Second)),
	}}); err != nil {
		t.Fatal(err)
	}
	first, err := ledger.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.AtHead || len(first.Changes) != 1 {
		t.Fatalf("first backup page = %#v", first)
	}
	if err := ledger.store.BackupContext(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}

	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	remaining, err := restored.ReadChanges(ctx, neutral.ReadChangesRequest{After: first.Next, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := changeIDs(remaining.Changes); !reflect.DeepEqual(got, []string{"event:feed:backup:2"}) || !remaining.AtHead {
		t.Fatalf("restored backup page = %#v", remaining)
	}
}

func TestChangeCursorV1StrictCanonicalEncoding(t *testing.T) {
	claims := changeCursorClaimsV1{
		Version:        neutral.ChangeCursorVersionV1,
		StoreID:        "store:v1:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IntegrityEpoch: "canonical-event-chain-v1",
		Position:       42,
	}
	cursor, err := encodeChangeCursorV1(claims)
	if err != nil {
		t.Fatal(err)
	}
	const want = "eventstore-change-cursor-v1.eyJ2ZXJzaW9uIjoiZXZlbnRzdG9yZS1jaGFuZ2UtY3Vyc29yLXYxIiwic3RvcmVfaWQiOiJzdG9yZTp2MTpzaGEyNTY6YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYSIsImludGVncml0eV9lcG9jaCI6ImNhbm9uaWNhbC1ldmVudC1jaGFpbi12MSIsInBvc2l0aW9uIjo0Mn0.280a21f9d734a7df849ffb69a356b14abec7168a1c7ec6a761c9101a82124707"
	if string(cursor) != want {
		t.Fatalf("cursor vector changed\n got: %s\nwant: %s", cursor, want)
	}
	decoded, err := decodeChangeCursorV1(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != claims {
		t.Fatalf("decoded claims = %#v, want %#v", decoded, claims)
	}

	nonCanonical := rawCursorForTest([]byte(`{"position":42,"version":"eventstore-change-cursor-v1","store_id":"store:v1:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","integrity_epoch":"canonical-event-chain-v1"}`))
	if _, err := decodeChangeCursorV1(nonCanonical); !errors.Is(err, neutral.ErrCursorInvalid) {
		t.Fatalf("non-canonical cursor error = %v", err)
	}
	unknownField := rawCursorForTest([]byte(`{"version":"eventstore-change-cursor-v1","store_id":"store:v1:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","integrity_epoch":"canonical-event-chain-v1","position":42,"extra":true}`))
	if _, err := decodeChangeCursorV1(unknownField); !errors.Is(err, neutral.ErrCursorInvalid) {
		t.Fatalf("unknown-field cursor error = %v", err)
	}
	oversized := neutral.ChangeCursor(neutral.ChangeCursorVersionV1 + "." + strings.Repeat("a", maxChangeCursorBytes) + "." + strings.Repeat("0", 64))
	if _, err := decodeChangeCursorV1(oversized); !errors.Is(err, neutral.ErrCursorInvalid) {
		t.Fatalf("oversized cursor error = %v", err)
	}
}

func TestChangeFeedDoesNotSkipUnsupportedAcceptedCodec(t *testing.T) {
	ctx := context.Background()
	ledger, err := Open(filepath.Join(t.TempDir(), "unsupported-codec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	begin, err := ledger.BeginChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream := neutral.Ref{Kind: "run", ID: "run:unsupported-codec"}
	native := toMissisEvent(spyEvent("event:unsupported-codec", stream, "spy.run.started", stream, `{}`, time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC)))
	if _, err := ledger.store.AppendBatch([]model.Event{native}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	page, err := ledger.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 10})
	if !errors.Is(err, neutral.ErrChangeRecordUnsupported) {
		t.Fatalf("unsupported codec error = %v", err)
	}
	if len(page.Changes) != 0 || page.Next != "" {
		t.Fatalf("unsupported codec returned usable page %#v", page)
	}
}

func TestChangeFeedRejectsAcceptedBytesDigestMismatch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "digest-mismatch.db")
	ledger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := ledger.BeginChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream := neutral.Ref{Kind: "run", ID: "run:digest-mismatch"}
	if _, err := ledger.Append(ctx, neutral.AppendRequest{IdempotencyKey: "digest-mismatch-v1", Events: []neutral.Event{
		spyEvent("event:digest-mismatch", stream, "spy.run.started", stream, `{}`, time.Date(2026, 8, 28, 13, 30, 0, 0, time.UTC)),
	}}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET content_hash=? WHERE id=?`, "sha256:"+strings.Repeat("0", 64), "event:digest-mismatch"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := ledger.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 10})
	if !errors.Is(err, neutral.ErrChangeFeedIntegrity) || !strings.Contains(err.Error(), "content digest mismatch") {
		t.Fatalf("digest mismatch error = %v", err)
	}
	if len(page.Changes) != 0 || page.Next != "" {
		t.Fatalf("digest mismatch returned usable page %#v", page)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChangeFeedRejectsOrdinalGapWithoutReturningPartialPage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ordinal-gap.db")
	ledger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := ledger.BeginChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream := neutral.Ref{Kind: "run", ID: "run:ordinal-gap"}
	at := time.Date(2026, 8, 28, 13, 40, 0, 0, time.UTC)
	if _, err := ledger.Append(ctx, neutral.AppendRequest{IdempotencyKey: "ordinal-gap-v1", Events: []neutral.Event{
		spyEvent("event:ordinal-gap:1", stream, "spy.run.started", stream, `{}`, at),
		spyEvent("event:ordinal-gap:2", stream, "spy.probe.observed", stream, `{}`, at.Add(time.Second)),
		spyEvent("event:ordinal-gap:3", stream, "spy.run.completed", stream, `{}`, at.Add(2*time.Second)),
	}}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM events WHERE id=?`, "event:ordinal-gap:2"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := ledger.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: 10})
	if !errors.Is(err, neutral.ErrChangeFeedIntegrity) || !strings.Contains(err.Error(), "expected accepted position 2") {
		t.Fatalf("ordinal gap error = %v", err)
	}
	if len(page.Changes) != 0 || page.Next != "" {
		t.Fatalf("ordinal gap returned partial page %#v", page)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
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

func rawCursorForTest(payload []byte) neutral.ChangeCursor {
	hash := sha256.New()
	_, _ = hash.Write(changeCursorDigestDomainV1)
	_, _ = hash.Write(payload)
	return neutral.ChangeCursor(neutral.ChangeCursorVersionV1 + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + hex.EncodeToString(hash.Sum(nil)))
}

func FuzzDecodeChangeCursorV1(f *testing.F) {
	valid, err := encodeChangeCursorV1(changeCursorClaimsV1{
		Version:        neutral.ChangeCursorVersionV1,
		StoreID:        "store:v1:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IntegrityEpoch: "canonical-event-chain-v1",
		Position:       42,
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(valid))
	f.Add("")
	f.Add("eventstore-change-cursor-v1.not-base64.not-a-digest")
	f.Add(strings.Repeat("x", maxChangeCursorBytes+1))
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = decodeChangeCursorV1(neutral.ChangeCursor(raw))
	})
}

func readAllChangesForTest(ctx context.Context, feed neutral.ChangeFeed, cursor neutral.ChangeCursor, limit uint32) ([]neutral.Event, error) {
	var events []neutral.Event
	for {
		page, err := feed.ReadChanges(ctx, neutral.ReadChangesRequest{After: cursor, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, change := range page.Changes {
			events = append(events, change.Event)
		}
		cursor = page.Next
		if page.AtHead {
			return events, nil
		}
	}
}
