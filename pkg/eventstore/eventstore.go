// Package eventstore preserves the original Missis import path while the
// canonical consumer-neutral API lives in the event-tooling module. New code
// should import github.com/ravinsharma7/skunkwork/packages/eventstore.
package eventstore

import neutral "github.com/ravinsharma7/skunkwork/packages/eventstore"

var (
	ErrIdempotencyMismatch = neutral.ErrIdempotencyMismatch
	ErrInvalidEvent        = neutral.ErrInvalidEvent
)

const (
	ProtocolVersionV3Alpha4 = neutral.ProtocolVersionV3Alpha4
	RecordCodecV1           = neutral.RecordCodecV1
	DefaultPayloadCodec     = neutral.DefaultPayloadCodec
)

type Ref = neutral.Ref
type Actor = neutral.Actor
type Event = neutral.Event
type AppendRequest = neutral.AppendRequest
type AppendResult = neutral.AppendResult
type Ledger = neutral.Ledger

func ValidateRef(ref Ref) error {
	return neutral.ValidateRef(ref)
}

func ValidateEvent(event Event) error {
	return neutral.ValidateEvent(event)
}

func CanonicalAcceptedEventBytesV1(event Event) ([]byte, error) {
	return neutral.CanonicalAcceptedEventBytesV1(event)
}

func DecodeAcceptedEventV1(data []byte) (Event, error) {
	return neutral.DecodeAcceptedEventV1(data)
}
