// Package eventstoreadapter adapts the current Missis SQLite implementation
// to the consumer-neutral extraction probe in pkg/eventstore.
package eventstoreadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
	neutral "github.com/ravinsharma7/missis/pkg/eventstore"
)

type Adapter struct {
	store *store.Store
}

var _ neutral.Ledger = (*Adapter)(nil)

func Open(path string) (*Adapter, error) {
	opened, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	return &Adapter{store: opened}, nil
}

func (a *Adapter) Close() error {
	return a.store.Close()
}

func (a *Adapter) StoreID(ctx context.Context) (string, error) {
	return a.store.StoreIDContext(ctx)
}

func (a *Adapter) Append(ctx context.Context, request neutral.AppendRequest) (neutral.AppendResult, error) {
	if len(request.Events) == 0 {
		return neutral.AppendResult{}, fmt.Errorf("%w: event batch is empty", neutral.ErrInvalidEvent)
	}
	converted := make([]model.Event, len(request.Events))
	for index, event := range request.Events {
		if err := neutral.ValidateEvent(event); err != nil {
			return neutral.AppendResult{}, fmt.Errorf("event %d: %w", index, err)
		}
		converted[index] = toMissisEvent(event)
	}
	outcome, err := a.store.AppendBatchContext(ctx, converted, request.IdempotencyKey, nil, nil)
	if err != nil {
		if errors.Is(err, store.ErrIdempotencyMismatch) {
			return neutral.AppendResult{}, fmt.Errorf("%w: %v", neutral.ErrIdempotencyMismatch, err)
		}
		return neutral.AppendResult{}, err
	}
	accepted, err := fromMissisEvents(outcome.Events)
	if err != nil {
		return neutral.AppendResult{}, err
	}
	return neutral.AppendResult{Replayed: outcome.Replayed, Events: accepted}, nil
}

func (a *Adapter) ReadStream(ctx context.Context, streamRef neutral.Ref) ([]neutral.Event, error) {
	if err := neutral.ValidateRef(streamRef); err != nil {
		return nil, err
	}
	events, err := a.store.LoadStreamEventsContext(ctx, model.Ref{Kind: model.Kind(streamRef.Kind), Entity: streamRef.ID})
	if err != nil {
		return nil, err
	}
	return fromMissisEvents(events)
}

func toMissisEvent(event neutral.Event) model.Event {
	return model.Event{
		ID:        model.EventID(event.ID),
		Stream:    model.Ref{Kind: model.Kind(event.Stream.Kind), Entity: event.Stream.ID},
		Operation: model.OpObserveEffect,
		Target: model.Ref{
			Kind:   model.KindPart,
			Entity: "neutral-event:" + event.ID,
		},
		Value: model.Value{
			Kind: model.ValueKindJSON,
			Text: event.Type,
			Data: string(event.Payload),
		},
		RecordedAt:  event.RecordedAt,
		EffectiveAt: event.EffectiveAt,
		Actor:       model.ActorRef{Kind: event.Actor.Kind, ID: event.Actor.ID},
		Inputs: []model.Ref{{
			Kind:   model.Kind(event.Subject.Kind),
			Entity: event.Subject.ID,
		}},
	}
}

func fromMissisEvents(events []model.Event) ([]neutral.Event, error) {
	result := make([]neutral.Event, len(events))
	for index, event := range events {
		if event.Operation != model.OpObserveEffect || len(event.Inputs) != 1 {
			return nil, fmt.Errorf("eventstore adapter: event %s is not a neutral envelope", event.ID)
		}
		payload, ok := event.Value.Data.(string)
		if !ok {
			return nil, fmt.Errorf("eventstore adapter: event %s payload is not exact text", event.ID)
		}
		result[index] = neutral.Event{
			ID:          string(event.ID),
			Stream:      neutral.Ref{Kind: string(event.Stream.Kind), ID: event.Stream.Entity},
			Type:        event.Value.Text,
			Subject:     neutral.Ref{Kind: string(event.Inputs[0].Kind), ID: event.Inputs[0].Entity},
			Payload:     []byte(payload),
			RecordedAt:  event.RecordedAt,
			EffectiveAt: event.EffectiveAt,
			Actor:       neutral.Actor{Kind: event.Actor.Kind, ID: event.Actor.ID},
		}
	}
	return result, nil
}
