package eventstoreadapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	neutral "github.com/ravinsharma7/missis/pkg/eventstore"
)

const cssRecorderBurstSize = 32

func TestCSSFlightRecorderSessionRoundTripUsesOnlyNeutralLedger(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "css-flight-recorder.db")
	ledger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 28, 8, 0, 0, 0, time.UTC)
	session := neutral.Ref{Kind: "browser-session", ID: "session:cssfr:fixture-1"}
	events := []neutral.Event{
		cssRecorderEvent("event:cssfr:start", session, "css.session.started", session,
			`{"browser":"chromium","capture_mode":"focused","max_snapshots":34}`, at),
		cssRecorderEvent("event:cssfr:initial", session, "css.observation.recorded",
			neutral.Ref{Kind: "browser-target", ID: "target:#submit"},
			`{"ordinal":0,"phase":"before","computed":{"opacity":"1"}}`, at.Add(time.Millisecond)),
	}
	for index := 1; index <= cssRecorderBurstSize; index++ {
		events = append(events, cssRecorderEvent(
			fmt.Sprintf("event:cssfr:observation:%02d", index),
			session,
			"css.observation.recorded",
			neutral.Ref{Kind: "browser-target", ID: "target:#submit"},
			fmt.Sprintf(`{"ordinal":%d,"phase":"after","computed":{"opacity":"0.8"}}`, index),
			at.Add(time.Duration(index+1)*time.Millisecond),
		))
	}
	artifact := neutral.Ref{
		Kind: "artifact",
		ID:   "artifact:sha256:7a4d73d2f67e18b31ad07463c20abf86dd42e8c9014a18dfc43745db7b18b481",
	}
	events = append(events,
		cssRecorderEvent("event:cssfr:artifact", session, "css.artifact.referenced", artifact,
			`{"role":"after-screenshot","media_type":"image/png","capture_ordinal":32}`, at.Add(34*time.Millisecond)),
		cssRecorderEvent("event:cssfr:stop", session, "css.session.completed", session,
			`{"status":"complete","observations":33,"artifact_references":1}`, at.Add(35*time.Millisecond)),
	)

	first, err := ledger.Append(ctx, neutral.AppendRequest{
		IdempotencyKey: "cssfr-session-fixture-v1",
		Events:         events,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed {
		t.Fatal("first append reported replay")
	}
	if len(first.Events) != cssRecorderBurstSize+4 {
		t.Fatalf("accepted events = %d, want %d", len(first.Events), cssRecorderBurstSize+4)
	}

	acceptedBatchID := ""
	for index, event := range first.Events {
		if event.ProtocolVersion != neutral.ProtocolVersionV3Alpha4 || event.Namespace == "" || event.StreamRevision != uint64(index+1) || event.BatchID == "" || event.RecordCodec != neutral.RecordCodecV1 {
			t.Fatalf("accepted authority fields at %d = %#v", index, event)
		}
		if acceptedBatchID == "" {
			acceptedBatchID = event.BatchID
		} else if event.BatchID != acceptedBatchID {
			t.Fatalf("accepted batch IDs differ: %q/%q", acceptedBatchID, event.BatchID)
		}
	}

	storedRecords, err := ledger.store.LoadAcceptedStreamRecordsContext(ctx, model.Ref{Kind: model.Kind(session.Kind), Entity: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(storedRecords) != len(first.Events) {
		t.Fatalf("stored accepted records = %d, want %d", len(storedRecords), len(first.Events))
	}
	for index, record := range storedRecords {
		wantBytes, err := neutral.CanonicalAcceptedEventBytesV1(first.Events[index])
		if err != nil {
			t.Fatal(err)
		}
		if record.RecordCodec != neutral.RecordCodecV1 || !bytes.Equal(record.AcceptedBytes, wantBytes) {
			t.Fatalf("stored CSS recorder envelope %d was reconstructed or changed", index)
		}
	}

	retry, err := ledger.Append(ctx, neutral.AppendRequest{
		IdempotencyKey: "cssfr-session-fixture-v1",
		Events:         events,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Replayed || !reflect.DeepEqual(retry.Events, first.Events) {
		t.Fatal("identical recorder retry did not replay the accepted batch")
	}
	changed := append([]neutral.Event(nil), events...)
	changed[2].Payload = []byte(`{"ordinal":1,"phase":"after","computed":{"opacity":"0.6"}}`)
	if _, err := ledger.Append(ctx, neutral.AppendRequest{IdempotencyKey: "cssfr-session-fixture-v1", Events: changed}); !errors.Is(err, neutral.ErrIdempotencyMismatch) {
		t.Fatalf("changed recorder retry error = %v, want idempotency mismatch", err)
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
	got, err := ledger.ReadStream(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, first.Events) {
		t.Fatalf("CSS recorder restart/readback mismatch\n got: %#v\nwant: %#v", got, first.Events)
	}
	if got[len(got)-2].Subject != artifact {
		t.Fatalf("artifact reference = %#v, want %#v", got[len(got)-2].Subject, artifact)
	}
}

func cssRecorderEvent(id string, stream neutral.Ref, eventType string, subject neutral.Ref, payload string, at time.Time) neutral.Event {
	return neutral.Event{
		ID:          id,
		Stream:      stream,
		Type:        eventType,
		Subject:     subject,
		Payload:     []byte(payload),
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       neutral.Actor{Kind: "facility", ID: "css-flight-recorder"},
	}
}
