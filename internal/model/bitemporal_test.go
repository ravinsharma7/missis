package model

import (
	"testing"
	"time"
)

const bitemporalTicket = TicketID("ticket:bt")

func bScalarEvent(id string, seq uint64, op Operation, rec, eff time.Time, text string, supersedes ...EventID) Event {
	return Event{
		ID:          EventID(id),
		Stream:      Ref{Kind: KindTicket, Entity: string(bitemporalTicket)},
		Sequence:    seq,
		Operation:   op,
		Target:      Ref{Kind: KindPart, Entity: "part:status", Path: []string{"status"}},
		Value:       Value{Kind: ValueKindStatus, Text: text},
		RecordedAt:  rec,
		EffectiveAt: eff,
		Actor:       ActorRef{Kind: "test", ID: "test"},
		Supersedes:  supersedes,
	}
}

func bStatusValue(proj *Projection) *string {
	part, ok := proj.Parts[PartID("part:status")]
	if !ok || part.Value == nil {
		return nil
	}
	text := part.Value.Text
	return &text
}

func TestEventOrderingUsesDeterministicTieBreakers(t *testing.T) {
	// covers N046
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e2", 2, OpSetValue, base, base, "second"),
		bScalarEvent("e1", 1, OpSetValue, base, base, "first"),
	}
	sortEventsByValidTime(events)
	if events[0].Sequence != 1 || events[1].Sequence != 2 {
		t.Fatalf("event order = %d, %d; want stream sequence order", events[0].Sequence, events[1].Sequence)
	}
}

func TestBitemporalNormalUpdate(t *testing.T) {
	// covers PH1-BT-001
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base, base, "open"),
		bScalarEvent("e2", 2, OpSetValue, base.Add(2*time.Hour), base.Add(2*time.Hour), "done"),
	}
	proj, err := ProjectTicket(events, bitemporalTicket, base.Add(3*time.Hour), base.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(proj); got == nil || *got != "done" {
		t.Fatalf("status = %v, want done", got)
	}
}

func TestBitemporalBackdatedUpdate(t *testing.T) {
	// covers PH1-BT-001
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base, base, "open"),
		bScalarEvent("e2", 2, OpSetValue, base.Add(2*time.Hour), base.Add(2*time.Hour), "done"),
		bScalarEvent("e3", 3, OpSetValue, base.Add(3*time.Hour), base.Add(1*time.Hour), "doing"),
	}
	mid, err := ProjectTicket(events, bitemporalTicket, base.Add(90*time.Minute), base.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(mid); got == nil || *got != "doing" {
		t.Fatalf("status at 11:30 = %v, want doing", got)
	}
	late, err := ProjectTicket(events, bitemporalTicket, base.Add(3*time.Hour), base.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(late); got == nil || *got != "done" {
		t.Fatalf("status at 13:00 = %v, want done", got)
	}
}

func TestBitemporalFutureUpdate(t *testing.T) {
	// covers PH1-BT-001
	base := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC) // Monday
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base, base, "open"),
		bScalarEvent("e2", 2, OpSetValue, base, base.Add(96*time.Hour), "done"), // effective Friday
	}
	wed, err := ProjectTicket(events, bitemporalTicket, base.Add(48*time.Hour), base.Add(72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(wed); got == nil || *got != "open" {
		t.Fatalf("status Wednesday = %v, want open", got)
	}
	sat, err := ProjectTicket(events, bitemporalTicket, base.Add(120*time.Hour), base.Add(72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(sat); got == nil || *got != "done" {
		t.Fatalf("status Saturday = %v, want done", got)
	}
}

func TestBitemporalSameEffectiveTime(t *testing.T) {
	// covers PH1-BT-001
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base, base.Add(2*time.Hour), "open"),
		bScalarEvent("e2", 2, OpSetValue, base.Add(1*time.Hour), base.Add(2*time.Hour), "done"),
	}
	proj, err := ProjectTicket(events, bitemporalTicket, base.Add(2*time.Hour), base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(proj); got == nil || *got != "done" {
		t.Fatalf("status = %v, want done (later recorded wins)", got)
	}
}

func TestBitemporalRetraction(t *testing.T) {
	// covers PH1-BT-001 PH1-BT-002
	base := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base.Add(1*time.Hour), base, "A"),
		bScalarEvent("e2", 2, OpRetractValue, base.Add(5*time.Hour), base.Add(2*time.Hour), ""),
	}
	known, err := ProjectTicket(events, bitemporalTicket, base.Add(3*time.Hour), base.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(known); got != nil {
		t.Fatalf("status after retraction = %v, want none", *got)
	}
	part := known.Parts[PartID("part:status")]
	if part == nil || part.RetractedBy == nil || *part.RetractedBy != "e2" {
		t.Fatalf("expected RetractedBy e2, got %+v", part)
	}
	before, err := ProjectTicket(events, bitemporalTicket, base.Add(3*time.Hour), base.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(before); got == nil || *got != "A" {
		t.Fatalf("status before retraction was known = %v, want A", got)
	}
}

func TestBitemporalBackdatedRetraction(t *testing.T) {
	// covers PH1-BT-001 PH1-BT-002
	base := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base.Add(1*time.Hour), base, "A"),
		bScalarEvent("e2", 2, OpRetractValue, base.Add(5*time.Hour), base.Add(90*time.Minute), ""),
	}
	known, err := ProjectTicket(events, bitemporalTicket, base.Add(105*time.Minute), base.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(known); got != nil {
		t.Fatalf("status after backdated retraction = %v, want none", *got)
	}
	before, err := ProjectTicket(events, bitemporalTicket, base.Add(105*time.Minute), base.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(before); got == nil || *got != "A" {
		t.Fatalf("status before retraction was known = %v, want A", got)
	}
}

func TestBitemporalSupersession(t *testing.T) {
	// covers PH1-BT-001 PH1-BT-003
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base, base, "A"),
		bScalarEvent("e2", 2, OpSetValue, base.Add(2*time.Hour), base.Add(1*time.Hour), "B", "e1"),
	}
	notKnown, err := ProjectTicket(events, bitemporalTicket, base.Add(30*time.Minute), base.Add(90*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(notKnown); got == nil || *got != "A" {
		t.Fatalf("status before superseder was known = %v, want A", got)
	}
	knownGap, err := ProjectTicket(events, bitemporalTicket, base.Add(30*time.Minute), base.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(knownGap); got != nil {
		t.Fatalf("status in superseded-but-not-effective gap = %v, want none", *got)
	}
	after, err := ProjectTicket(events, bitemporalTicket, base.Add(3*time.Hour), base.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(after); got == nil || *got != "B" {
		t.Fatalf("status after superseder effective = %v, want B", got)
	}
}

func TestBitemporalOutOfOrderImport(t *testing.T) {
	// covers PH1-BT-001 PH1-BT-004
	base := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base.Add(1*time.Hour), base.Add(4*time.Hour), "done"),
		bScalarEvent("e2", 2, OpSetValue, base, base.Add(2*time.Hour), "open"),
	}
	for _, reversed := range []bool{false, true} {
		ordered := append([]Event(nil), events...)
		if reversed {
			ordered[0], ordered[1] = ordered[1], ordered[0]
		}
		mid, err := ProjectTicket(ordered, bitemporalTicket, base.Add(3*time.Hour), base.Add(4*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if got := bStatusValue(mid); got == nil || *got != "open" {
			t.Fatalf("reversed=%v status at 11:00 = %v, want open", reversed, got)
		}
		late, err := ProjectTicket(ordered, bitemporalTicket, base.Add(5*time.Hour), base.Add(4*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if got := bStatusValue(late); got == nil || *got != "done" {
			t.Fatalf("reversed=%v status at 13:00 = %v, want done", reversed, got)
		}
	}
}

func TestBitemporalBackdatedMove(t *testing.T) {
	// covers PH1-BT-004
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	stream := Ref{Kind: KindTicket, Entity: string(bitemporalTicket)}
	actor := ActorRef{Kind: "test", ID: "test"}
	pRef := Ref{Kind: KindPart, Entity: "part:p", Path: []string{"p"}}
	xRef := Ref{Kind: KindPart, Entity: "part:x", Path: []string{"x"}}
	events := []Event{
		{ID: EventID("e1"), Stream: stream, Sequence: 1, Operation: OpCreatePart, Target: pRef, Value: Value{Kind: ValueKindText, Text: "p"}, RecordedAt: base, EffectiveAt: base, Actor: actor},
		{ID: EventID("e2"), Stream: stream, Sequence: 2, Operation: OpCreatePart, Target: xRef, Value: Value{Kind: ValueKindText, Text: "x"}, RecordedAt: base, EffectiveAt: base, Actor: actor},
		{ID: EventID("e3"), Stream: stream, Sequence: 3, Operation: OpMovePart, Target: pRef, Value: Value{Ref: &xRef}, RecordedAt: base.Add(3 * time.Hour), EffectiveAt: base.Add(1 * time.Hour), Actor: actor},
	}
	before, err := ProjectTicket(events, bitemporalTicket, base.Add(30*time.Minute), base.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := before.Paths["p"]; !ok {
		t.Fatalf("expected p at root before move, paths=%v", before.Paths)
	}
	if _, ok := before.Paths["x/p"]; ok {
		t.Fatalf("unexpected x/p before move: %v", before.Paths)
	}
	after, err := ProjectTicket(events, bitemporalTicket, base.Add(90*time.Minute), base.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Paths["x/p"]; !ok {
		t.Fatalf("expected p under x after move, paths=%v", after.Paths)
	}
	if _, ok := after.Paths["p"]; ok {
		t.Fatalf("p should no longer be at root after move: %v", after.Paths)
	}
}

func TestBitemporalBackdatedLinkRetraction(t *testing.T) {
	// covers PH1-BT-004
	base := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	streamA := Ref{Kind: KindTicket, Entity: "ticket:a"}
	from := Ref{Kind: KindTicket, Entity: "ticket:a"}
	to := Ref{Kind: KindTicket, Entity: "ticket:b"}
	actor := ActorRef{Kind: "test", ID: "test"}
	assert := Event{
		ID: EventID("e1"), Stream: streamA, Sequence: 1, Operation: OpAssertLink,
		Target: from, Value: Value{Text: "blocked-by", Ref: &to},
		RecordedAt: base.Add(1 * time.Hour), EffectiveAt: base, Actor: actor,
	}
	retract := Event{
		ID: EventID("e2"), Stream: streamA, Sequence: 2, Operation: OpRetractLink,
		Target: from, Value: Value{Text: "blocked-by", Ref: &to},
		RecordedAt: base.Add(4 * time.Hour), EffectiveAt: base.Add(90 * time.Minute), Actor: actor,
	}
	events := []Event{assert, retract}

	known, err := LinksForRef(events, from, base.Add(2*time.Hour), base.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(known) != 0 {
		t.Fatalf("link should be retracted when effective and known, got %+v", known)
	}
	notKnown, err := LinksForRef(events, from, base.Add(2*time.Hour), base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(notKnown) != 1 {
		t.Fatalf("link should be active before retraction was known, got %+v", notKnown)
	}
}

func TestBitemporalKnownTimeBoundaryInclusive(t *testing.T) {
	// covers PH1-BT-001: the recorded_at <= knownAt boundary is inclusive.
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base, base, "open"),
		bScalarEvent("e2", 2, OpSetValue, base.Add(2*time.Hour), base.Add(1*time.Hour), "doing"),
	}
	inclusive, err := ProjectTicket(events, bitemporalTicket, base.Add(3*time.Hour), base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(inclusive); got == nil || *got != "doing" {
		t.Fatalf("status at exact known boundary = %v, want doing", got)
	}
	exclusive, err := ProjectTicket(events, bitemporalTicket, base.Add(3*time.Hour), base.Add(2*time.Hour).Add(-time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(exclusive); got == nil || *got != "open" {
		t.Fatalf("status one nanosecond before known boundary = %v, want open", got)
	}
}

func TestBitemporalBackdatedUpdateKnownWindow(t *testing.T) {
	// covers PH1-BT-001 PH1-BT-004: a plain backdated update is invisible
	// until it is recorded, then wins by effective time once known.
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	events := []Event{
		bScalarEvent("e1", 1, OpSetValue, base, base, "open"),
		bScalarEvent("e2", 2, OpSetValue, base.Add(4*time.Hour), base.Add(1*time.Hour), "doing"),
	}
	unknown, err := ProjectTicket(events, bitemporalTicket, base.Add(2*time.Hour), base.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(unknown); got == nil || *got != "open" {
		t.Fatalf("status before backdated update was recorded = %v, want open", got)
	}
	known, err := ProjectTicket(events, bitemporalTicket, base.Add(2*time.Hour), base.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := bStatusValue(known); got == nil || *got != "doing" {
		t.Fatalf("status after backdated update was recorded = %v, want doing", got)
	}
}
