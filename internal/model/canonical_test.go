package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func canonicalVectorEvent() Event {
	return Event{
		ID:          EventID("event:01H7XYZ"),
		Stream:      Ref{Kind: KindTicket, Entity: "ticket:abc"},
		Sequence:    7,
		Operation:   OpSetValue,
		Target:      Ref{Kind: KindPart, Entity: "part:status", Path: []string{"status"}},
		Value:       Value{Kind: ValueKindStatus, Text: "doing"},
		RecordedAt:  time.Date(2026, 8, 17, 2, 40, 29, 123456789, time.UTC),
		EffectiveAt: time.Date(2026, 8, 17, 2, 40, 29, 120000000, time.UTC),
		Actor:       ActorRef{Kind: "human", ID: "human/local"},
		Reason:      "update",
	}
}

// canonicalVectorJSON is the expected output written independently of the
// encoder implementation, per the contract in the main specification. It is
// the published cleanroom vector.
const canonicalVectorJSON = `{"ID":"event:01H7XYZ","Stream":{"Kind":"ticket","Entity":"ticket:abc","Path":null},"Sequence":7,"BatchID":null,"Operation":"set-value","Target":{"Kind":"part","Entity":"part:status","Path":["status"]},"Value":{"Kind":"status","Text":"doing","Data":null,"List":null,"Ref":null,"Retracted":false},"RecordedAt":"2026-08-17T02:40:29.123456789Z","EffectiveAt":"2026-08-17T02:40:29.120000000Z","Actor":{"Kind":"human","ID":"human/local","Name":""},"Sources":null,"Inputs":null,"Causes":null,"Effects":null,"Supersedes":null,"Reason":"update","Ontologies":null,"Invocation":null}`

func TestCanonicalEventBytesV1Vector(t *testing.T) {
	// covers PH1-ENC-001
	got, err := CanonicalEventBytesV1(canonicalVectorEvent())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != canonicalVectorJSON {
		t.Fatalf("canonical bytes mismatch:\ngot:  %s\nwant: %s", got, canonicalVectorJSON)
	}
}

func TestCanonicalEventBytesV1ExcludesDerivedFields(t *testing.T) {
	base := canonicalVectorEvent()
	base.AliasSeq = 99
	base.PreviousHash = "deadbeef"
	base.Hash = "cafebabe"
	got, err := CanonicalEventBytesV1(base)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != canonicalVectorJSON {
		t.Fatalf("derived fields must be excluded from canonical bytes:\ngot:  %s\nwant: %s", got, canonicalVectorJSON)
	}
}

func TestCanonicalEventBytesV1NoHTMLEscaping(t *testing.T) {
	event := canonicalVectorEvent()
	event.Value.Text = "<a&b>"
	got, err := CanonicalEventBytesV1(event)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"Text":"<a&b>"`) {
		t.Fatalf("canonical bytes must not HTML-escape: %s", got)
	}
}

// TestCanonicalEventBytesV1NoRawNUL pins the safety of the 0x00 hash domain
// framing: canonical JSON escapes NUL as \u0000, so the variable fields in
// the hash input (hex previous_hash, JSON canonical bytes) can never contain
// a raw NUL byte that could shift the framing boundaries.
func TestCanonicalEventBytesV1NoRawNUL(t *testing.T) {
	event := canonicalVectorEvent()
	event.Value.Text = "a\x00b"
	got, err := CanonicalEventBytesV1(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range got {
		if b == 0 {
			t.Fatalf("canonical bytes must not contain a raw NUL byte: %q", got)
		}
	}
	if !strings.Contains(string(got), `"Text":"a\u0000b"`) {
		t.Fatalf("NUL must be JSON-escaped in canonical bytes: %s", got)
	}
}

func TestCanonicalEventBytesV1MapKeysSorted(t *testing.T) {
	event := canonicalVectorEvent()
	event.Value.Kind = ValueKindJSON
	event.Value.Data = map[string]any{"b": 1, "a": 2}
	got, err := CanonicalEventBytesV1(event)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"Data":{"a":2,"b":1}`) {
		t.Fatalf("map keys must be sorted in canonical bytes: %s", got)
	}
}

func TestComputeEventHashV1Vector(t *testing.T) {
	// covers PH1-ENC-001
	event := canonicalVectorEvent()
	previous := "0000000000000000000000000000000000000000000000000000000000000000"

	// Independent computation of the documented framing, separate from the
	// implementation under test.
	input := []byte("MISSIS-EVENT-HASH\x00v1\x00")
	input = append(input, []byte(previous)...)
	input = append(input, 0)
	input = append(input, []byte(canonicalVectorJSON)...)
	sum := sha256.Sum256(input)
	want := hex.EncodeToString(sum[:])

	got, err := ComputeEventHashV1(event, previous)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("hash mismatch:\ngot:  %s\nwant: %s", got, want)
	}

	// Determinism.
	again, err := ComputeEventHashV1(event, previous)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("hash must be deterministic: %s != %s", again, got)
	}

	// One-byte sensitivity: a different previous hash must change the result.
	other, err := ComputeEventHashV1(event, strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	if other == got {
		t.Fatal("hash must depend on previous_hash")
	}
}

func TestCanonicalTimeTrailingZerosAndUTC(t *testing.T) {
	// 120ms in a +08:00 zone must render as UTC with nine digits.
	zone := time.FixedZone("+08", 8*60*60)
	ts := time.Date(2026, 8, 17, 10, 40, 29, 120000000, zone)
	if got := canonicalTime(ts); got != "2026-08-17T02:40:29.120000000Z" {
		t.Fatalf("canonicalTime = %s", got)
	}
}
