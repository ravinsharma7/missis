// Package eventstore defines the smallest consumer-neutral ledger surface
// currently proven by Missis and the Spy Testing fixture. It deliberately
// excludes Missis tickets, parts, projections, aliases, and CLI concepts.
package eventstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrIdempotencyMismatch = errors.New("eventstore: idempotency request mismatch")
	ErrInvalidEvent        = errors.New("eventstore: invalid event")
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
// retained byte-for-byte by the current adapter; interpreting it belongs to
// the consumer.
type Event struct {
	ID          string    `json:"id"`
	Stream      Ref       `json:"stream"`
	Type        string    `json:"type"`
	Subject     Ref       `json:"subject"`
	Payload     []byte    `json:"payload"`
	RecordedAt  time.Time `json:"recorded_at"`
	EffectiveAt time.Time `json:"effective_at"`
	Actor       Actor     `json:"actor"`
}

type AppendRequest struct {
	IdempotencyKey string  `json:"idempotency_key"`
	Events         []Event `json:"events"`
}

type AppendResult struct {
	Replayed bool    `json:"replayed"`
	Events   []Event `json:"events"`
}

// Ledger is the alpha extraction probe. New methods require executable
// evidence from another consumer before this interface is promoted to a
// shared package.
type Ledger interface {
	Append(context.Context, AppendRequest) (AppendResult, error)
	ReadStream(context.Context, Ref) ([]Event, error)
	StoreID(context.Context) (string, error)
	Close() error
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
