package missis

import (
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/ravinsharma7/missis/implementation/model"
)

func NewID(prefix string) string {
	return prefix + ":" + ulid.Make().String()
}

func NewEvent(stream model.Ref, operation model.Operation, target model.Ref, value model.Value, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, reason string) model.Event {
	return model.Event{
		ID:          model.EventID(NewID("event")),
		Stream:      stream,
		Operation:   operation,
		Target:      target,
		Value:       value,
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
		BatchID:     &batchID,
		Reason:      reason,
	}
}

func PartEvent(stream model.Ref, path string, value any, kind model.ValueKind, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID) model.Event {
	partID := model.PartID(NewID("part"))
	target := model.Ref{Kind: model.KindPart, Entity: string(partID), Path: splitPath(path)}
	return model.Event{
		ID:          model.EventID(NewID("event")),
		Stream:      stream,
		Operation:   model.OpCreatePart,
		Target:      target,
		Value:       valueForPart(path, value, kind),
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
		BatchID:     &batchID,
	}
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	parts := make([]string, 0)
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	return parts
}

func valueForPart(path string, value any, kind model.ValueKind) model.Value {
	v := model.Value{Kind: kind}
	switch typed := value.(type) {
	case string:
		v.Text = typed
	case []string:
		v.List = append([]string(nil), typed...)
	case nil:
		return v
	default:
		v.Data = typed
	}
	return v
}
