package model

import (
	"reflect"
	"testing"
	"time"
)

func TestOrderedChildrenUsesOrderKeysAndLegacyFallback(t *testing.T) {
	stream := Ref{Kind: KindTicket, Entity: "ticket:ordered"}
	actor := ActorRef{Kind: "human", ID: "tester"}
	base := time.Unix(0, 0).UTC()
	parent := Ref{Kind: KindPart, Entity: "parent", Path: []string{"body"}}
	first := Ref{Kind: KindPart, Entity: "first", Path: []string{"body", "first"}}
	second := Ref{Kind: KindPart, Entity: "second", Path: []string{"body", "second"}}
	legacy := Ref{Kind: KindPart, Entity: "legacy", Path: []string{"body", "legacy"}}
	events := []Event{
		{ID: "e1", Stream: stream, Sequence: 1, Operation: OpCreatePart, Target: parent, RecordedAt: base, EffectiveAt: base, Actor: actor},
		{ID: "e2", Stream: stream, Sequence: 2, Operation: OpCreatePart, Target: first, Value: Value{Ref: &parent, OrderKey: OrderKeyForIndex(1)}, RecordedAt: base, EffectiveAt: base, Actor: actor},
		{ID: "e3", Stream: stream, Sequence: 3, Operation: OpCreatePart, Target: second, Value: Value{Ref: &parent, OrderKey: OrderKeyForIndex(0)}, RecordedAt: base, EffectiveAt: base, Actor: actor},
		{ID: "e4", Stream: stream, Sequence: 4, Operation: OpCreatePart, Target: legacy, Value: Value{Ref: &parent}, RecordedAt: base, EffectiveAt: base, Actor: actor},
	}
	projection, err := ProjectTicket(events, TicketID(stream.Entity), base, base)
	if err != nil {
		t.Fatal(err)
	}
	children := OrderedChildren(projection, partIDPtr(PartID("parent")))
	got := make([]PartID, 0, len(children))
	for _, part := range children {
		got = append(got, part.ID)
	}
	if want := []PartID{"second", "first", "legacy"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered children = %v, want %v", got, want)
	}
}

func TestOrderKeyMidpointIsSortable(t *testing.T) {
	left := OrderKeyForIndex(0)
	right := OrderKeyForIndex(1)
	middle, err := BetweenOrderKeys(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if !(left < middle && middle < right) {
		t.Fatalf("middle key %q is not between %q and %q", middle, left, right)
	}
}

func partIDPtr(id PartID) *PartID { return &id }
