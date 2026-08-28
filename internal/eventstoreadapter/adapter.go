// Package eventstoreadapter adapts the current Missis SQLite implementation
// to the consumer-neutral extraction probe in pkg/eventstore.
package eventstoreadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/ravinsharma7/missis/internal/idgen"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
	neutral "github.com/ravinsharma7/skunkwork/packages/eventstore"
)

type Adapter struct {
	store *store.Store
}

var _ neutral.Ledger = (*Adapter)(nil)
var _ neutral.ChangeFeed = (*Adapter)(nil)

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
	if request.IdempotencyKey != "" {
		requestHash, err := store.ComputeIdempotencyRequestHashV1(struct {
			Operation string
			Events    []neutral.Event
		}{Operation: "eventstore-append-v1", Events: request.Events})
		if err != nil {
			return neutral.AppendResult{}, err
		}
		ctx = store.WithIdempotencyRequestHash(ctx, requestHash)
	}
	converted := make([]model.Event, len(request.Events))
	originalByID := make(map[string]neutral.Event, len(request.Events))
	var acceptedBatchID *model.BatchID
	if len(request.Events) > 1 {
		batchID := model.BatchID(idgen.New("batch"))
		acceptedBatchID = &batchID
	}
	for index, event := range request.Events {
		if err := neutral.ValidateEvent(event); err != nil {
			return neutral.AppendResult{}, fmt.Errorf("event %d: %w", index, err)
		}
		converted[index] = toMissisEvent(event)
		converted[index].BatchID = acceptedBatchID
		originalByID[event.ID] = event
	}
	storeID, err := a.store.StoreIDContext(ctx)
	if err != nil {
		return neutral.AppendResult{}, err
	}
	encode := func(assigned model.Event) (string, []byte, error) {
		original, ok := originalByID[string(assigned.ID)]
		if !ok {
			return "", nil, fmt.Errorf("eventstore adapter: no neutral proposal for event %s", assigned.ID)
		}
		accepted := authorityAcceptedEvent(original, assigned, storeID)
		data, err := neutral.CanonicalAcceptedEventBytesV1(accepted)
		return neutral.RecordCodecV1, data, err
	}
	outcome, err := a.store.AppendEncodedBatchContext(ctx, converted, request.IdempotencyKey, nil, nil, encode)
	if err != nil {
		if errors.Is(err, store.ErrIdempotencyMismatch) {
			return neutral.AppendResult{}, fmt.Errorf("%w: %v", neutral.ErrIdempotencyMismatch, err)
		}
		return neutral.AppendResult{}, err
	}
	accepted := make([]neutral.Event, len(outcome.Events))
	for index, assigned := range outcome.Events {
		original, ok := originalByID[string(assigned.ID)]
		if !ok {
			return neutral.AppendResult{}, fmt.Errorf("eventstore adapter: replay returned unknown event %s", assigned.ID)
		}
		accepted[index] = authorityAcceptedEvent(original, assigned, storeID)
	}
	return neutral.AppendResult{Replayed: outcome.Replayed, Events: accepted}, nil
}

func (a *Adapter) ReadStream(ctx context.Context, streamRef neutral.Ref) ([]neutral.Event, error) {
	if err := neutral.ValidateRef(streamRef); err != nil {
		return nil, err
	}
	records, err := a.store.LoadAcceptedStreamRecordsContext(ctx, model.Ref{Kind: model.Kind(streamRef.Kind), Entity: streamRef.ID})
	if err != nil {
		return nil, err
	}
	events := make([]neutral.Event, len(records))
	for index, record := range records {
		if record.RecordCodec != neutral.RecordCodecV1 {
			return nil, fmt.Errorf("eventstore adapter: event %s uses unsupported codec %q; exact bytes preserved", record.EventID, record.RecordCodec)
		}
		if computed := model.EventContentHashV1(record.AcceptedBytes); record.ContentHash != computed {
			return nil, fmt.Errorf("eventstore adapter: event %s content digest mismatch: stored=%q computed=%q", record.EventID, record.ContentHash, computed)
		}
		decoded, err := neutral.DecodeAcceptedEventV1(record.AcceptedBytes)
		if err != nil {
			return nil, fmt.Errorf("eventstore adapter: decode event %s: %w", record.EventID, err)
		}
		events[index] = decoded
	}
	return events, nil
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

func authorityAcceptedEvent(original neutral.Event, assigned model.Event, storeID string) neutral.Event {
	accepted := original
	accepted.ProtocolVersion = neutral.ProtocolVersionV3Alpha4
	accepted.Namespace = storeID
	accepted.StreamRevision = assigned.Sequence
	if assigned.BatchID != nil {
		accepted.BatchID = string(*assigned.BatchID)
	} else {
		accepted.BatchID = ""
	}
	accepted.RecordedAt = assigned.RecordedAt
	accepted.EffectiveAt = assigned.EffectiveAt
	accepted.RecordCodec = neutral.RecordCodecV1
	if accepted.SchemaVersion == 0 {
		accepted.SchemaVersion = 1
	}
	if accepted.PayloadCodec == "" {
		accepted.PayloadCodec = neutral.DefaultPayloadCodec
	}
	return accepted
}
