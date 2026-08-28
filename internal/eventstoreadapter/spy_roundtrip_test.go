package eventstoreadapter

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	neutral "github.com/ravinsharma7/skunkwork/packages/eventstore"
)

func TestSpyRunRoundTripUsesOnlyNeutralLedger(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "spy.db")
	ledger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 28, 5, 45, 0, 0, time.UTC)
	run := neutral.Ref{Kind: "run", ID: "run:spy:fixture-1"}
	events := []neutral.Event{
		spyEvent("event:spy:1", run, "spy.run.started", run, `{"source_capsule_ref":"artifact:sha256:source"}`, at),
		spyEvent("event:spy:2", run, "spy.probe.observed", neutral.Ref{Kind: "probe", ID: "probe:checkout"}, `{"phase":"after","value":42}`, at.Add(time.Second)),
		spyEvent("event:spy:3", run, "spy.evidence.attached", neutral.Ref{Kind: "artifact", ID: "artifact:sha256:evidence"}, `{"media_type":"application/json","complete":true}`, at.Add(2*time.Second)),
		spyEvent("event:spy:4", run, "spy.run.completed", run, `{"status":"passed","coverage":"complete"}`, at.Add(3*time.Second)),
	}

	first, err := appendSpyRun(ctx, ledger, "spy-run-fixture-v1", events)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed {
		t.Fatal("first append reported replay")
	}
	accepted := first.Events
	acceptedBatchID := ""
	for index, event := range accepted {
		if event.ProtocolVersion != neutral.ProtocolVersionV3Alpha4 || event.Namespace == "" || event.StreamRevision != uint64(index+1) || event.BatchID == "" || event.RecordCodec != neutral.RecordCodecV1 {
			t.Fatalf("accepted authority fields at %d = %#v", index, event)
		}
		if acceptedBatchID == "" {
			acceptedBatchID = event.BatchID
		} else if event.BatchID != acceptedBatchID {
			t.Fatalf("accepted batch IDs differ: %q/%q", acceptedBatchID, event.BatchID)
		}
	}
	storedRecords, err := ledger.store.LoadAcceptedStreamRecordsContext(ctx, modelRef(run))
	if err != nil {
		t.Fatal(err)
	}
	if len(storedRecords) != len(accepted) {
		t.Fatalf("stored accepted records = %d, want %d", len(storedRecords), len(accepted))
	}
	for index, record := range storedRecords {
		wantBytes, err := neutral.CanonicalAcceptedEventBytesV1(accepted[index])
		if err != nil {
			t.Fatal(err)
		}
		if record.RecordCodec != neutral.RecordCodecV1 || !bytes.Equal(record.AcceptedBytes, wantBytes) {
			t.Fatalf("stored neutral envelope %d was reconstructed or changed", index)
		}
	}
	retry, err := appendSpyRun(ctx, ledger, "spy-run-fixture-v1", events)
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Replayed {
		t.Fatal("identical retry did not replay")
	}

	changed := append([]neutral.Event(nil), events...)
	changed[1].Payload = []byte(`{"phase":"after","value":43}`)
	if _, err := appendSpyRun(ctx, ledger, "spy-run-fixture-v1", changed); !errors.Is(err, neutral.ErrIdempotencyMismatch) {
		t.Fatalf("changed retry error = %v, want idempotency mismatch", err)
	}

	storeID, err := ledger.StoreID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if storeID == "" {
		t.Fatal("empty store identity")
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}

	ledger, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	got, err := ledger.ReadStream(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, accepted) {
		t.Fatalf("round trip mismatch\n got: %#v\nwant: %#v", got, accepted)
	}
}

func TestSpyExactEnvelopeRejectsChangedCompatibilityProjectionInput(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "spy-tamper.db")
	ledger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	run := neutral.Ref{Kind: "run", ID: "run:spy:tamper"}
	event := spyEvent("event:spy:tamper", run, "spy.probe.observed", neutral.Ref{Kind: "probe", ID: "probe:tamper"}, `{"value":42}`, time.Date(2026, 8, 28, 6, 0, 0, 0, time.UTC))
	if _, err := appendSpyRun(ctx, ledger, "spy-tamper-v1", []neutral.Event{event}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET event_json=replace(event_json,'spy.probe.observed','spy.probe.forged')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "compatibility indexes disagree with exact neutral envelope") {
		t.Fatalf("Open error = %v, want exact-envelope compatibility mismatch", err)
	}
}

func modelRef(ref neutral.Ref) model.Ref {
	return model.Ref{Kind: model.Kind(ref.Kind), Entity: ref.ID}
}

func appendSpyRun(ctx context.Context, ledger neutral.Ledger, key string, events []neutral.Event) (neutral.AppendResult, error) {
	return ledger.Append(ctx, neutral.AppendRequest{IdempotencyKey: key, Events: events})
}

func spyEvent(id string, stream neutral.Ref, eventType string, subject neutral.Ref, payload string, at time.Time) neutral.Event {
	return neutral.Event{
		ID:          id,
		Stream:      stream,
		Type:        eventType,
		Subject:     subject,
		Payload:     []byte(payload),
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       neutral.Actor{Kind: "facility", ID: "spy-testing"},
	}
}
