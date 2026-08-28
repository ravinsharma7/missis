package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// CanonicalEventBytesV1 returns the canonical JSON byte form of an event
// defined by specs/missues-issue-specification.v2.md section 10.10.
//
// The output is language-independent and deterministic: fixed top-level field
// order, UTC timestamps with exactly nine fractional digits and trailing
// zeros retained, no HTML escaping, no duplicate keys. Nested object key
// order follows the reference Go types; the published test vectors pin the
// exact bytes for cleanroom implementations.
func CanonicalEventBytesV1(event Event) ([]byte, error) {
	event.Value = NormalizeValueDataForCanonical(event.Value)
	var b bytes.Buffer
	b.WriteByte('{')
	if err := writeCanonicalField(&b, "ID", event.ID); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Stream", event.Stream); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Sequence", event.Sequence); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "BatchID", event.BatchID); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Operation", event.Operation); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Target", event.Target); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Value", event.Value); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "RecordedAt", canonicalTime(event.RecordedAt)); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "EffectiveAt", canonicalTime(event.EffectiveAt)); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Actor", event.Actor); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Sources", event.Sources); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Inputs", event.Inputs); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Causes", event.Causes); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Effects", event.Effects); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Supersedes", event.Supersedes); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Reason", event.Reason); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Ontologies", event.Ontologies); err != nil {
		return nil, err
	}
	if err := writeCanonicalField(&b, "Invocation", event.Invocation); err != nil {
		return nil, err
	}
	// Drop the trailing comma left by the per-field writer.
	if n := b.Len(); n > 0 && b.Bytes()[n-1] == ',' {
		b.Truncate(n - 1)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// ComputeEventHashV1 hashes an event with the domain-separated framing from
// the specification:
//
//	HashInput = "MISSIS-EVENT-HASH" || 0x00 || "v1" || 0x00
//	            || previous_hash || 0x00 || canonical_bytes
//
// The 0x00 framing is unambiguous because neither variable field can contain
// a raw NUL: previous_hash is lowercase hex, and canonical bytes are JSON,
// which escapes NUL as \u0000 (see TestCanonicalEventBytesV1NoRawNUL).
func ComputeEventHashV1(event Event, previousHash string) (string, error) {
	canonical, err := CanonicalEventBytesV1(event)
	if err != nil {
		return "", err
	}
	return ComputeEventHashBytesV1(canonical, previousHash), nil
}

// ComputeEventHashBytesV1 applies the canonical-v1 chain framing directly to
// exact bytes already accepted by the authority. Verification must use this
// function rather than decode and re-encode historical accepted bytes.
func ComputeEventHashBytesV1(canonical []byte, previousHash string) string {
	input := append([]byte("MISSIS-EVENT-HASH\x00v1\x00"), []byte(previousHash)...)
	input = append(input, 0)
	input = append(input, canonical...)
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}

// EventContentHashV1 is the direct portable identity of exact accepted record
// bytes. The integrity-chain hash is separate because it also binds the prior
// head. The algorithm prefix makes the stored value self-describing.
func EventContentHashV1(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeCanonicalField(b *bytes.Buffer, name string, value any) error {
	encoded, err := encodeCanonicalJSON(value)
	if err != nil {
		return fmt.Errorf("canonical field %s: %w", name, err)
	}
	b.WriteString(`"` + name + `":`)
	b.Write(encoded)
	b.WriteByte(',')
	return nil
}

func encodeCanonicalJSON(value any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// canonicalTime renders an instant as UTC with exactly nine fractional
// digits and trailing zeros retained, e.g. 2026-08-17T02:40:29.120000000Z.
func canonicalTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
