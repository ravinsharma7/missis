package model

import (
	"strings"
	"testing"
	"time"
)

func TestProjectionRenameMoveRetract(t *testing.T) {
	t.Parallel()
	// covers PH1-PART-001 PH1-PART-003 PH1-PART-004 PH1-PART-005 PH1-PART-006 PH1-PRJ-005 PH1-DM-001
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
	// covers PH1-PART-006 PH1-PART-007 PH1-EVT-008
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
	// covers PH1-PART-011 PH1-REF-004
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
	// covers PH1-PART-011
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
	// covers PH1-PRJ-005
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

func TestLinksForRef(t *testing.T) {
	t.Parallel()
	// covers PH2-LINK-001 PH2-LINK-002 PH2-LINK-003
	ticketA := TicketID("ticket:a")
	ticketB := TicketID("ticket:b")
	streamA := Ref{Kind: KindTicket, Entity: string(ticketA)}
	actor := ActorRef{Kind: "test", ID: "test", Name: "test"}
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	from := Ref{Kind: KindTicket, Entity: string(ticketA)}
	to := Ref{Kind: KindTicket, Entity: string(ticketB)}

	assert := Event{
		ID:          EventID("event:link:1"),
		Stream:      streamA,
		Sequence:    1,
		Operation:   OpAssertLink,
		Target:      from,
		Value:       Value{Text: "blocked-by", Ref: &to},
		RecordedAt:  base,
		EffectiveAt: base,
		Actor:       actor,
	}
	events := []Event{assert}
	links, err := LinksForRef(events, from, base.Add(time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Relation != "blocked-by" || links[0].Direction != "asserted" {
		t.Fatalf("unexpected links: %+v", links)
	}

	incoming, err := LinksForRef(events, to, base.Add(time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(incoming) != 1 || incoming[0].Relation != "blocks" || incoming[0].Direction != "derived-inverse" {
		t.Fatalf("unexpected inverse links: %+v", incoming)
	}

	retract := Event{
		ID:          EventID("event:link:2"),
		Stream:      streamA,
		Sequence:    2,
		Operation:   OpRetractLink,
		Target:      from,
		Value:       Value{Text: "blocked-by", Ref: &to},
		RecordedAt:  base.Add(time.Second),
		EffectiveAt: base.Add(time.Second),
		Actor:       actor,
	}
	events = append(events, retract)
	links, err = LinksForRef(events, from, base.Add(time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no current links after retraction, got %+v", links)
	}
}

func TestLineageWalk(t *testing.T) {
	t.Parallel()
	// covers PH2-LINEAGE-001 PH2-LINEAGE-002
	ticketA := TicketID("ticket:a")
	ticketB := TicketID("ticket:b")
	ticketC := TicketID("ticket:c")
	actor := ActorRef{Kind: "test", ID: "test", Name: "test"}
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	events := []Event{
		linkEvent(ticketA, ticketB, "blocked-by", actor, base, 1),
		linkEvent(ticketB, ticketC, "caused-by", actor, base.Add(time.Second), 2),
	}
	graph, err := BuildLineageGraph(events, base.Add(time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	start := Ref{Kind: KindTicket, Entity: string(ticketA)}
	edges, err := graph.Walk(start, "both", 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d: %+v", len(edges), edges)
	}
	if edges[0].Depth != 1 || edges[1].Depth != 2 {
		t.Fatalf("unexpected depths: %+v", edges)
	}

	filtered, err := graph.Walk(start, "outgoing", 1, map[string]bool{"blocked-by": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected one filtered edge, got %d", len(filtered))
	}
}

func TestParseMarkdownParts(t *testing.T) {
	t.Parallel()
	content := `# Retry after shutdown

## Problem

The worker retries after shutdown.

## Evidence

### Race test

Failed on iteration 417.
`
	parts, err := ParseMarkdownParts(content)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"retry-after-shutdown":                    "",
		"retry-after-shutdown/problem":            "The worker retries after shutdown.",
		"retry-after-shutdown/evidence":           "",
		"retry-after-shutdown/evidence/race-test": "Failed on iteration 417.",
	}
	if len(parts) != len(want) {
		t.Fatalf("got %d parts, want %d", len(parts), len(want))
	}
	for _, part := range parts {
		key := strings.Join(part.Path, "/")
		if want[key] != part.Body {
			t.Fatalf("part %s body = %q, want %q", key, part.Body, want[key])
		}
	}
}

func linkEvent(from, to TicketID, relation string, actor ActorRef, effectiveAt time.Time, sequence uint64) Event {
	toRef := Ref{Kind: KindTicket, Entity: string(to)}
	return Event{
		ID:          EventID("event:" + string(from) + ":" + relation + ":" + string(to)),
		Stream:      Ref{Kind: KindTicket, Entity: string(from)},
		Sequence:    sequence,
		Operation:   OpAssertLink,
		Target:      Ref{Kind: KindTicket, Entity: string(from)},
		Value:       Value{Text: relation, Ref: &toRef},
		RecordedAt:  effectiveAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
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
