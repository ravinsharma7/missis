package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

func TestAppendBatchMultiStream(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir(), "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	streamA := model.Ref{Kind: model.KindProject, Entity: "a"}
	streamB := model.Ref{Kind: model.KindProject, Entity: "b"}
	events := []model.Event{
		{
			Stream: streamA, Operation: model.OpCreateEntity, Target: streamA,
			Value:      model.Value{Kind: model.ValueKindText, Text: "A"},
			RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{ID: "human/local"},
		},
		{
			Stream: streamB, Operation: model.OpCreateEntity, Target: streamB,
			Value:      model.Value{Kind: model.ValueKindText, Text: "B"},
			RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{ID: "human/local"},
		},
	}
	outcome, err := s.AppendBatch(events, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Events) != 2 {
		t.Fatalf("events appended = %d", len(outcome.Events))
	}
	all, err := s.LoadEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("stored events = %d", len(all))
	}
	byStream := map[string]uint64{}
	for _, event := range all {
		key := string(event.Stream.Kind) + ":" + event.Stream.Entity
		byStream[key] = event.Sequence
	}
	if byStream["project:a"] != 1 || byStream["project:b"] != 1 {
		t.Fatalf("per-stream sequences = %+v, want 1 for both", byStream)
	}
}

func TestLinkPrecondition(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir(), "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	from := model.Ref{Kind: model.KindProject, Entity: "a"}
	to := model.Ref{Kind: model.KindTicket, Entity: "ticket:01J5"}
	linkEvent := func(op model.Operation, id model.EventID) model.Event {
		return model.Event{
			ID: id, Stream: from, Operation: op, Target: from,
			Value:      model.Value{Text: "contains", Ref: &to},
			RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{ID: "human/local"},
		}
	}

	assertion := linkEvent(model.OpAssertLink, "")
	outcome, err := s.AppendBatch([]model.Event{assertion}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Events) != 1 {
		t.Fatalf("assertion events = %d", len(outcome.Events))
	}
	current := outcome.Events[0].ID

	// Matching precondition succeeds.
	retract := linkEvent(model.OpRetractLink, "")
	_, err = s.AppendBatch([]model.Event{retract}, "", []Precondition{{
		Link: &LinkPrecondition{From: from, Relation: "contains", To: to, ExpectedCurrentEvent: current},
	}}, nil)
	if err != nil {
		t.Fatalf("matching precondition should pass: %v", err)
	}

	// Stale precondition conflicts.
	assertAgain := linkEvent(model.OpAssertLink, "")
	if _, err := s.AppendBatch([]model.Event{assertAgain}, "", []Precondition{{
		Link: &LinkPrecondition{From: from, Relation: "contains", To: to, ExpectedCurrentEvent: current},
	}}, nil); err != ErrConflict {
		t.Fatalf("stale precondition should conflict, got %v", err)
	}
}

func TestLinkPreconditionMultipleAssertions(t *testing.T) {
	t.Parallel()
	s, err := Open(filepath.Join(t.TempDir(), "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	from := model.Ref{Kind: model.KindProject, Entity: "a"}
	to := model.Ref{Kind: model.KindTicket, Entity: "ticket:01J5"}
	assert := func() model.Event {
		return model.Event{
			Stream: from, Operation: model.OpAssertLink, Target: from,
			Value:      model.Value{Text: "contains", Ref: &to},
			RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{ID: "human/local"},
		}
	}
	first, err := s.AppendBatch([]model.Event{assert()}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendBatch([]model.Event{assert()}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	// A precondition on the first assertion passes while both are active.
	retract := model.Event{
		Stream: from, Operation: model.OpRetractLink, Target: from,
		Value:      model.Value{Text: "contains", Ref: &to},
		RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{ID: "human/local"},
		Causes: []model.Ref{{Kind: model.KindEvent, Entity: string(first.Events[0].ID)}},
	}
	if _, err := s.AppendBatch([]model.Event{retract}, "", []Precondition{{
		Link: &LinkPrecondition{From: from, Relation: "contains", To: to, ExpectedCurrentEvent: first.Events[0].ID},
	}}, nil); err != nil {
		t.Fatalf("precondition on an active assertion should pass: %v", err)
	}
}
