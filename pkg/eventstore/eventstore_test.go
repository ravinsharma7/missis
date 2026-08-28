package eventstore

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateEventRejectsMissingNeutralIdentity(t *testing.T) {
	event := Event{
		ID:          "event:1",
		Stream:      Ref{Kind: "run", ID: "run:1"},
		Type:        "spy.run.started",
		Subject:     Ref{Kind: "run", ID: "run:1"},
		Payload:     []byte(`{}`),
		RecordedAt:  time.Now().UTC(),
		EffectiveAt: time.Now().UTC(),
		Actor:       Actor{Kind: "facility"},
	}
	if err := ValidateEvent(event); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v, want invalid event", err)
	}
}

func TestCanonicalAcceptedEventBytesV1RoundTripAndRejectUnknownFields(t *testing.T) {
	event := Event{
		ProtocolVersion: ProtocolVersionV3Alpha4,
		Namespace:       "store:v1:sha256:fixture",
		ID:              "event:neutral:1",
		Stream:          Ref{Kind: "run", ID: "run:neutral:1"},
		StreamRevision:  7,
		BatchID:         "batch:neutral:fixture",
		Type:            "spy.probe.observed",
		SchemaVersion:   1,
		Subject:         Ref{Kind: "probe", ID: "probe:checkout"},
		PayloadCodec:    DefaultPayloadCodec,
		Payload:         []byte(`{"html":"<exact>"}`),
		RecordedAt:      time.Date(2026, 8, 28, 1, 2, 3, 120_000_000, time.FixedZone("fixture", 8*60*60)),
		EffectiveAt:     time.Date(2026, 8, 28, 1, 2, 3, 120_000_000, time.FixedZone("fixture", 8*60*60)),
		Actor:           Actor{Kind: "facility", ID: "spy-testing"},
		RecordCodec:     RecordCodecV1,
	}
	encoded, err := CanonicalAcceptedEventBytesV1(event)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`\u003c`)) || !bytes.Contains(encoded, []byte(`2026-08-27T17:02:03.120000000Z`)) {
		t.Fatalf("canonical neutral bytes normalized unexpectedly: %s", encoded)
	}
	decoded, err := DecodeAcceptedEventV1(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := CanonicalAcceptedEventBytesV1(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Fatalf("round trip changed accepted bytes\n got: %s\nwant: %s", reencoded, encoded)
	}
	withUnknown := []byte(strings.Replace(string(encoded), `"record_id":`, `"future":"preserved","record_id":`, 1))
	if _, err := DecodeAcceptedEventV1(withUnknown); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
}
