package model

import (
	"strings"
	"testing"
	"time"
)

func TestProjectionRenameMoveRetract(t *testing.T) {
	t.Parallel()
	ticket := TicketID("ticket:t")
	stream := Ref{Kind: KindTicket, Entity: string(ticket)}
	actor := ActorRef{Kind: "test", ID: "test", Name: "test"}
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	titleID := PartID("part:title")
	statusID := PartID("part:status")
	raceID := PartID("part:race")

	events := []Event{
		partCreateEvent(stream, titleID, []string{"title"}, nil, Value{Kind: ValueKindText, Text: "Issue"}, actor, base, 1),
		partCreateEvent(stream, statusID, []string{"status"}, nil, Value{Kind: ValueKindStatus, Text: "open"}, actor, base, 2),
		partCreateEvent(stream, raceID, []string{"evidence", "race-test"}, &statusID, Value{Kind: ValueKindText, Text: "go test"}, actor, base.Add(time.Second), 3),
		simpleEvent(stream, OpRenamePart, Ref{Kind: KindPart, Entity: string(raceID)}, Value{Kind: ValueKindText, Text: "race-detector"}, actor, base.Add(2*time.Second), 4),
		simpleEvent(stream, OpMovePart, Ref{Kind: KindPart, Entity: string(raceID)}, Value{Ref: &Ref{Kind: KindPart, Entity: string(statusID)}}, actor, base.Add(3*time.Second), 5),
	}

	proj, err := CurrentProjection(events, ticket, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if got := proj.Parts[titleID].Value.Text; got != "Issue" {
		t.Fatalf("title = %q", got)
	}
	if _, ok := proj.Paths["status/race-detector"]; !ok {
		t.Fatalf("expected moved path, paths=%v", proj.Paths)
	}

	events = append(events, simpleEvent(stream, OpRetractValue, Ref{Kind: KindPart, Entity: string(raceID)}, Value{}, actor, base.Add(4*time.Second), 6))
	proj, err = CurrentProjection(events, ticket, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("project after retract: %v", err)
	}
	if proj.Parts[raceID].Value != nil {
		t.Fatalf("expected retracted value to be nil")
	}

	events = append(events, simpleEvent(stream, OpRetractSubtree, Ref{Kind: KindPart, Entity: string(raceID)}, Value{}, actor, base.Add(5*time.Second), 7))
	proj, err = CurrentProjection(events, ticket, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("project after subtree retract: %v", err)
	}
	if _, ok := proj.Parts[raceID]; ok {
		t.Fatalf("expected retracted subtree to be absent")
	}
}

func TestValidateAppendRejectsCycleAndCollision(t *testing.T) {
	t.Parallel()
	ticket := TicketID("ticket:t")
	stream := Ref{Kind: KindTicket, Entity: string(ticket)}
	actor := ActorRef{Kind: "test", ID: "test", Name: "test"}
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	a := PartID("part:a")
	b := PartID("part:b")
	c := PartID("part:c")
	events := []Event{
		partCreateEvent(stream, a, []string{"a"}, nil, Value{}, actor, base, 1),
		partCreateEvent(stream, b, []string{"a", "b"}, &a, Value{}, actor, base, 2),
	}

	cycle := simpleEvent(stream, OpMovePart, Ref{Kind: KindPart, Entity: string(a)}, Value{Ref: &Ref{Kind: KindPart, Entity: string(b)}}, actor, base.Add(time.Second), 3)
	if err := ValidateAppend(events, cycle); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}

	events = append(events, partCreateEvent(stream, c, []string{"c"}, nil, Value{}, actor, base.Add(2*time.Second), 4))
	collision := simpleEvent(stream, OpRenamePart, Ref{Kind: KindPart, Entity: string(c)}, Value{Kind: ValueKindText, Text: "a"}, actor, base.Add(3*time.Second), 5)
	if err := ValidateAppend(events, collision); err == nil {
		t.Fatalf("expected path collision error")
	}
}

func TestResolvePartPath(t *testing.T) {
	t.Parallel()
	ticket := TicketID("ticket:t")
	stream := Ref{Kind: KindTicket, Entity: string(ticket)}
	actor := ActorRef{Kind: "test", ID: "test", Name: "test"}
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	partID := PartID("part:p")
	events := []Event{
		partCreateEvent(stream, partID, []string{"race-test"}, nil, Value{Kind: ValueKindText, Text: "x"}, actor, base, 1),
	}
	proj, err := CurrentProjection(events, ticket, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	resolved, err := ResolvePartPath(proj, ticket, []string{"race-test"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.PartID != partID {
		t.Fatalf("part = %s, want %s", resolved.PartID, partID)
	}
}

func TestValidatePathSegmentsRejectsInvalid(t *testing.T) {
	t.Parallel()
	valid := []string{"evidence", "race-test", "run-417"}
	if err := ValidatePathSegments(valid); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	for _, invalid := range [][]string{
		{""},
		{"-bad"},
		{"bad segment"},
		{"bad", "has space"},
	} {
		if err := ValidatePathSegments(invalid); err == nil {
			t.Fatalf("expected invalid path to fail: %v", invalid)
		}
	}
}

func TestReproducibleProjection(t *testing.T) {
	t.Parallel()
	ticket := TicketID("ticket:t")
	stream := Ref{Kind: KindTicket, Entity: string(ticket)}
	actor := ActorRef{Kind: "test", ID: "test", Name: "test"}
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	partID := PartID("part:p")
	events := []Event{
		partCreateEvent(stream, partID, []string{"title"}, nil, Value{Kind: ValueKindText, Text: "same"}, actor, base, 1),
	}
	first, err := CurrentProjection(events, ticket, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("first projection: %v", err)
	}
	second, err := CurrentProjection(events, ticket, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("second projection: %v", err)
	}
	if first.Paths["title"] != second.Paths["title"] {
		t.Fatalf("projection not reproducible")
	}
}

func partCreateEvent(stream Ref, id PartID, path []string, parent *PartID, value Value, actor ActorRef, effectiveAt time.Time, sequence uint64) Event {
	var parentRef *Ref
	if parent != nil {
		parentRef = &Ref{Kind: KindPart, Entity: string(*parent)}
	}
	if parentRef != nil {
		value.Ref = parentRef
	}
	return Event{
		ID:          EventID("event:" + string(id)),
		Stream:      stream,
		Sequence:    sequence,
		Operation:   OpCreatePart,
		Target:      Ref{Kind: KindPart, Entity: string(id), Path: path},
		Value:       value,
		RecordedAt:  effectiveAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
	}
}

func simpleEvent(stream Ref, operation Operation, target Ref, value Value, actor ActorRef, effectiveAt time.Time, sequence uint64) Event {
	return Event{
		ID:          EventID("event:" + string(operation) + ":" + target.Entity),
		Stream:      stream,
		Sequence:    sequence,
		Operation:   operation,
		Target:      target,
		Value:       value,
		RecordedAt:  effectiveAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
	}
}
