// Package eventstore defines the consumer-neutral ledger surface proven by
// Missis, Spy Testing, and CSS Flight Recorder. It deliberately excludes all
// consumer projections, filesystem locations, and physical adapter concepts.
package eventstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrIdempotencyMismatch      = errors.New("eventstore: idempotency request mismatch")
	ErrInvalidEvent             = errors.New("eventstore: invalid event")
	ErrChangeLimitInvalid       = errors.New("eventstore: change page limit invalid")
	ErrCursorInvalid            = errors.New("eventstore: change cursor invalid")
	ErrCursorCorrupt            = errors.New("eventstore: change cursor corrupt")
	ErrCursorVersionUnsupported = errors.New("eventstore: change cursor version unsupported")
	ErrCursorForeignStore       = errors.New("eventstore: change cursor belongs to another store")
	ErrCursorEpochMismatch      = errors.New("eventstore: change cursor integrity epoch mismatch")
	ErrCursorFuture             = errors.New("eventstore: change cursor is ahead of store")
	ErrCursorStale              = errors.New("eventstore: change cursor precedes retained history")
	ErrChangeRecordUnsupported  = errors.New("eventstore: accepted change record codec unsupported")
	ErrChangeFeedIntegrity      = errors.New("eventstore: accepted change feed integrity failure")
)

const (
	ProtocolVersionV3Alpha4        = "eventstore-v3-alpha.4"
	RecordCodecV1                  = "eventstore-record-json-v1"
	DefaultPayloadCodec            = "application/json"
	ChangeCursorVersionV1          = "eventstore-change-cursor-v1"
	MaxChangePageSize       uint32 = 1000
)

// Ref is an opaque typed identity. Paths, repository aliases, and filesystem
// locations are intentionally absent from accepted identity.
type Ref struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Event is an immutable domain event accepted into one stream. Payload is
// retained byte-for-byte; interpreting it belongs to the consumer.
type Event struct {
	ProtocolVersion string    `json:"protocol_version,omitempty"`
	Namespace       string    `json:"namespace,omitempty"`
	ID              string    `json:"id"`
	Stream          Ref       `json:"stream"`
	StreamRevision  uint64    `json:"stream_revision,omitempty"`
	BatchID         string    `json:"batch_id,omitempty"`
	Type            string    `json:"type"`
	SchemaVersion   uint32    `json:"schema_version,omitempty"`
	Subject         Ref       `json:"subject"`
	PayloadCodec    string    `json:"payload_codec,omitempty"`
	Payload         []byte    `json:"payload"`
	RecordedAt      time.Time `json:"recorded_at"`
	EffectiveAt     time.Time `json:"effective_at"`
	Actor           Actor     `json:"actor"`
	RecordCodec     string    `json:"record_codec,omitempty"`
}

type AppendRequest struct {
	IdempotencyKey string  `json:"idempotency_key"`
	Events         []Event `json:"events"`
}

type AppendResult struct {
	Replayed bool    `json:"replayed"`
	Events   []Event `json:"events"`
}

// ChangeCursor is an opaque authority-issued checkpoint. Consumers persist
// and return it unchanged; its representation is a versioned protocol detail,
// not a database position or filesystem locator.
type ChangeCursor string

type ReadChangesRequest struct {
	After ChangeCursor `json:"after"`
	Limit uint32       `json:"limit"`
}

type Change struct {
	Cursor ChangeCursor `json:"cursor"`
	Event  Event        `json:"event"`
}

type ChangePage struct {
	Changes []Change     `json:"changes"`
	Next    ChangeCursor `json:"next"`
	AtHead  bool         `json:"at_head"`
}

// Ledger is the smallest shared alpha interface confirmed by all three
// consumers. New methods require executable cross-consumer evidence.
type Ledger interface {
	Append(context.Context, AppendRequest) (AppendResult, error)
	ReadStream(context.Context, Ref) ([]Event, error)
	StoreID(context.Context) (string, error)
	Close() error
}

// ChangeFeed is an optional bounded, read-only traversal capability. It is
// separate from Ledger so simple adapters do not imply resumable feed support.
type ChangeFeed interface {
	BeginChanges(context.Context) (ChangeCursor, error)
	ReadChanges(context.Context, ReadChangesRequest) (ChangePage, error)
	LatestCursor(context.Context) (ChangeCursor, error)
}

func ValidateRef(ref Ref) error {
	if strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.ID) == "" {
		return fmt.Errorf("%w: reference kind and id are required", ErrInvalidEvent)
	}
	if ref.Kind != strings.TrimSpace(ref.Kind) || ref.ID != strings.TrimSpace(ref.ID) {
		return fmt.Errorf("%w: reference kind and id must not have surrounding whitespace", ErrInvalidEvent)
	}
	return nil
}

func ValidateEvent(event Event) error {
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidEvent)
	}
	if err := ValidateRef(event.Stream); err != nil {
		return err
	}
	if err := ValidateRef(event.Subject); err != nil {
		return err
	}
	if strings.TrimSpace(event.Type) == "" || event.Type != strings.TrimSpace(event.Type) {
		return fmt.Errorf("%w: type is required without surrounding whitespace", ErrInvalidEvent)
	}
	if event.Payload == nil {
		return fmt.Errorf("%w: payload is required", ErrInvalidEvent)
	}
	if event.RecordedAt.IsZero() || event.EffectiveAt.IsZero() {
		return fmt.Errorf("%w: recorded_at and effective_at are required", ErrInvalidEvent)
	}
	if strings.TrimSpace(event.Actor.ID) == "" {
		return fmt.Errorf("%w: actor id is required", ErrInvalidEvent)
	}
	return nil
}

type acceptedEventWireV1 struct {
	ProtocolVersion string `json:"protocol_version"`
	Namespace       string `json:"namespace"`
	RecordID        string `json:"record_id"`
	SchemaID        string `json:"schema_id"`
	SchemaVersion   uint32 `json:"schema_version"`
	Stream          Ref    `json:"stream"`
	StreamRevision  uint64 `json:"stream_revision"`
	BatchID         string `json:"batch_id"`
	Subject         Ref    `json:"subject"`
	RecordedAt      string `json:"recorded_at"`
	EffectiveAt     string `json:"effective_at"`
	Actor           Actor  `json:"actor"`
	RecordCodec     string `json:"record_codec"`
	PayloadCodec    string `json:"payload_codec"`
	Payload         []byte `json:"payload_bytes"`
}

// CanonicalAcceptedEventBytesV1 encodes the complete authority-accepted
// neutral envelope once, after namespace, stream revision, and timestamps are
// assigned. Chain/content hashes are outside these bytes to avoid circular
// input.
func CanonicalAcceptedEventBytesV1(event Event) ([]byte, error) {
	if err := ValidateEvent(event); err != nil {
		return nil, err
	}
	if event.ProtocolVersion != ProtocolVersionV3Alpha4 || strings.TrimSpace(event.Namespace) == "" || event.StreamRevision == 0 || event.RecordCodec != RecordCodecV1 {
		return nil, fmt.Errorf("%w: accepted authority fields are incomplete", ErrInvalidEvent)
	}
	if event.SchemaVersion == 0 || strings.TrimSpace(event.PayloadCodec) == "" {
		return nil, fmt.Errorf("%w: schema_version and payload_codec are required", ErrInvalidEvent)
	}
	wire := acceptedEventWireV1{
		ProtocolVersion: event.ProtocolVersion,
		Namespace:       event.Namespace,
		RecordID:        event.ID,
		SchemaID:        event.Type,
		SchemaVersion:   event.SchemaVersion,
		Stream:          event.Stream,
		StreamRevision:  event.StreamRevision,
		BatchID:         event.BatchID,
		Subject:         event.Subject,
		RecordedAt:      event.RecordedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		EffectiveAt:     event.EffectiveAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		Actor:           event.Actor,
		RecordCodec:     event.RecordCodec,
		PayloadCodec:    event.PayloadCodec,
		Payload:         event.Payload,
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(wire); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

// DecodeAcceptedEventV1 strictly decodes the named v1 codec. Unknown fields
// remain safely preserved in storage but require a newer decoder.
func DecodeAcceptedEventV1(data []byte) (Event, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire acceptedEventWireV1
	if err := decoder.Decode(&wire); err != nil {
		return Event{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Event{}, fmt.Errorf("accepted event contains trailing data")
	}
	recordedAt, err := time.Parse("2006-01-02T15:04:05.000000000Z", wire.RecordedAt)
	if err != nil {
		return Event{}, fmt.Errorf("recorded_at: %w", err)
	}
	effectiveAt, err := time.Parse("2006-01-02T15:04:05.000000000Z", wire.EffectiveAt)
	if err != nil {
		return Event{}, fmt.Errorf("effective_at: %w", err)
	}
	event := Event{
		ProtocolVersion: wire.ProtocolVersion, Namespace: wire.Namespace, ID: wire.RecordID,
		Stream: wire.Stream, StreamRevision: wire.StreamRevision, BatchID: wire.BatchID, Type: wire.SchemaID, SchemaVersion: wire.SchemaVersion,
		Subject: wire.Subject, PayloadCodec: wire.PayloadCodec, Payload: wire.Payload,
		RecordedAt: recordedAt, EffectiveAt: effectiveAt, Actor: wire.Actor, RecordCodec: wire.RecordCodec,
	}
	if _, err := CanonicalAcceptedEventBytesV1(event); err != nil {
		return Event{}, err
	}
	return event, nil
}
