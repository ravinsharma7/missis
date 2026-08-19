package model

import (
	"testing"
	"time"
)

func TestEvidenceSemanticsProjection(t *testing.T) {
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	stream := Ref{Kind: KindProject, Entity: "a"}
	to := Ref{Kind: KindTicket, Entity: "t1"}
	assert := func(id string, actor string) Event {
		return Event{
			ID: EventID(id), Stream: stream, Operation: OpAssertLink, Target: stream,
			Value:      Value{Text: "contains", Ref: &to},
			RecordedAt: base, EffectiveAt: base, Actor: ActorRef{ID: actor},
		}
	}
	retract := func(id string, target EventID) Event {
		return Event{
			ID: EventID(id), Stream: stream, Operation: OpRetractLink, Target: stream,
			Value:      Value{Text: "contains", Ref: &to},
			RecordedAt: base, EffectiveAt: base, Actor: ActorRef{ID: "human/local"},
			Causes: []Ref{{Kind: KindEvent, Entity: string(target)}},
		}
	}

	events := []Event{assert("e1", "human/local"), assert("e2", "plugin/x")}
	proj, err := ProjectStream(events, stream, base, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Links) != 2 {
		t.Fatalf("assertions = %d, want 2", len(proj.Links))
	}

	views, err := LinksForRef(events, stream, base, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || len(views[0].Assertions) != 2 {
		t.Fatalf("views = %+v, want 1 view with 2 assertions", views)
	}
	if views[0].Assertions[0].Actor.ID != "human/local" || views[0].Assertions[1].Actor.ID != "plugin/x" {
		t.Fatalf("assertion actors = %+v", views[0].Assertions)
	}

	// Retracting one assertion keeps the relation visible.
	oneRetracted, err := ProjectStream(append(events, retract("e3", "e1")), stream, base, base)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, link := range oneRetracted.Links {
		if link.RetractedBy == nil {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active assertions after one retraction = %d, want 1", active)
	}

	// Retracting the remaining assertion hides the relation.
	allEvents := append(append(events, retract("e3", "e1")), retract("e4", "e2"))
	allRetracted, err := ProjectStream(allEvents, stream, base, base)
	if err != nil {
		t.Fatal(err)
	}
	if views, err := LinksForRef(allEvents, stream, base, base); err == nil && len(views) != 0 {
		t.Fatalf("relation should be hidden after all retractions: %+v", views)
	}
	_ = allRetracted
}

func TestLegacyRetractionAppliesToFirstActiveAssertion(t *testing.T) {
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	stream := Ref{Kind: KindProject, Entity: "a"}
	to := Ref{Kind: KindTicket, Entity: "t1"}
	assert := func(id string) Event {
		return Event{
			ID: EventID(id), Stream: stream, Operation: OpAssertLink, Target: stream,
			Value:      Value{Text: "contains", Ref: &to},
			RecordedAt: base, EffectiveAt: base, Actor: ActorRef{ID: "human/local"},
		}
	}
	// Legacy pre-#66 retract has no assertion target.
	legacyRetract := Event{
		ID: "e3", Stream: stream, Operation: OpRetractLink, Target: stream,
		Value:      Value{Text: "contains", Ref: &to},
		RecordedAt: base, EffectiveAt: base, Actor: ActorRef{ID: "human/local"},
	}
	events := append([]Event{assert("e1"), assert("e2")}, legacyRetract)
	proj, err := ProjectStream(events, stream, base, base)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, link := range proj.Links {
		if link.RetractedBy == nil {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("legacy retraction should retract the first active assertion, active = %d", active)
	}
}

func TestEvidenceSemanticsHistoricalView(t *testing.T) {
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	stream := Ref{Kind: KindProject, Entity: "a"}
	to := Ref{Kind: KindTicket, Entity: "t1"}
	assert := Event{
		ID: "e1", Stream: stream, Operation: OpAssertLink, Target: stream,
		Value:      Value{Text: "contains", Ref: &to},
		RecordedAt: base, EffectiveAt: base, Actor: ActorRef{ID: "human/local"},
	}
	retract := Event{
		ID: "e2", Stream: stream, Operation: OpRetractLink, Target: stream,
		Value:      Value{Text: "contains", Ref: &to},
		RecordedAt: base.Add(2 * time.Hour), EffectiveAt: base.Add(2 * time.Hour),
		Actor: ActorRef{ID: "human/local"}, Causes: []Ref{{Kind: KindEvent, Entity: "e1"}},
	}
	events := []Event{assert, retract}
	views, err := LinksForRef(events, stream, base.Add(time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || len(views[0].Assertions) != 1 {
		t.Fatalf("historical view before retraction should show the assertion: %+v", views)
	}
}
