package model

import (
	"strings"
	"testing"
	"time"
)

var linkTestActor = ActorRef{Kind: "test", ID: "test", Name: "test"}

func linkTestAt() time.Time {
	return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
}

func linkPartEvent(stream Ref, id PartID, path []string, seq uint64, at time.Time) Event {
	return Event{
		ID:          EventID("event:part:" + string(id)),
		Stream:      stream,
		Sequence:    seq,
		Operation:   OpCreatePart,
		Target:      Ref{Kind: KindPart, Entity: string(id), Path: path},
		Value:       Value{Kind: ValueKindText, Text: strings.Join(path, "/")},
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       linkTestActor,
	}
}

func linkAssertEvent(stream Ref, from PartID, fromPath []string, to PartID, toPath []string, relation string, seq uint64, at time.Time) Event {
	toRef := Ref{Kind: KindPart, Entity: string(to), Path: toPath}
	return Event{
		ID:          EventID("event:link:" + string(from) + ":" + string(to)),
		Stream:      stream,
		Sequence:    seq,
		Operation:   OpAssertLink,
		Target:      Ref{Kind: KindPart, Entity: string(from), Path: fromPath},
		Value:       Value{Text: relation, Ref: &toRef},
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       linkTestActor,
	}
}

func linkRetractEvent(stream Ref, from PartID, fromPath []string, to PartID, toPath []string, relation string, seq uint64, at time.Time) Event {
	toRef := Ref{Kind: KindPart, Entity: string(to), Path: toPath}
	return Event{
		ID:          EventID("event:retract:" + string(from) + ":" + string(to)),
		Stream:      stream,
		Sequence:    seq,
		Operation:   OpRetractLink,
		Target:      Ref{Kind: KindPart, Entity: string(from), Path: fromPath},
		Value:       Value{Text: relation, Ref: &toRef},
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       linkTestActor,
	}
}

func linkRenameEvent(stream Ref, id PartID, oldPath []string, newName string, seq uint64, at time.Time) Event {
	return Event{
		ID:          EventID("event:rename:" + string(id)),
		Stream:      stream,
		Sequence:    seq,
		Operation:   OpRenamePart,
		Target:      Ref{Kind: KindPart, Entity: string(id), Path: oldPath},
		Value:       Value{Kind: ValueKindText, Text: newName},
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       linkTestActor,
	}
}

func linkMoveEvent(stream Ref, id PartID, oldPath []string, parent PartID, seq uint64, at time.Time) Event {
	parentRef := Ref{Kind: KindPart, Entity: string(parent)}
	return Event{
		ID:          EventID("event:move:" + string(id)),
		Stream:      stream,
		Sequence:    seq,
		Operation:   OpMovePart,
		Target:      Ref{Kind: KindPart, Entity: string(id), Path: oldPath},
		Value:       Value{Ref: &parentRef},
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       linkTestActor,
	}
}

func TestCanonicalRefKeyPathInsensitiveAndCollisionFree(t *testing.T) {
	at := linkTestAt()
	oldPath := Ref{Kind: KindPart, Entity: "part:P", Path: []string{"evidence", "race-test"}}
	newPath := Ref{Kind: KindPart, Entity: "part:P", Path: []string{"verification", "race-test"}}
	if CanonicalRefKey(oldPath) != CanonicalRefKey(newPath) {
		t.Fatalf("canonical key must ignore path: %q vs %q", CanonicalRefKey(oldPath), CanonicalRefKey(newPath))
	}
	if PresentationRefKey(oldPath) == PresentationRefKey(newPath) {
		t.Fatalf("presentation key must include path")
	}
	// Length-prefixing must be collision-free even when field content could
	// contain separator-like characters.
	cases := []struct {
		a, b Ref
	}{
		{Ref{Kind: KindPart, Entity: "P"}, Ref{Kind: KindPart, Entity: "P:1"}},
		{Ref{Kind: "a\x00b", Entity: "c"}, Ref{Kind: "a", Entity: "b\x00c"}},
		{Ref{Kind: KindPart, Entity: "P"}, Ref{Kind: KindProject, Entity: "P"}},
	}
	seen := make(map[string]string)
	for _, tc := range cases {
		for _, ref := range []Ref{tc.a, tc.b} {
			key := CanonicalRefKey(ref)
			repr := string(ref.Kind) + "|" + ref.Entity
			if prev, ok := seen[key]; ok && prev != repr {
				t.Fatalf("canonical key collision: %q", key)
			}
			seen[key] = repr
		}
	}
	_ = at
}

func TestRenameSurvival(t *testing.T) {
	at := linkTestAt()
	stream := Ref{Kind: KindTicket, Entity: "ticket:T1"}
	const p, q = PartID("part:P"), PartID("part:Q")
	events := []Event{
		linkPartEvent(stream, p, []string{"evidence", "race-test"}, 1, at),
		linkPartEvent(stream, q, []string{"hypothesis"}, 2, at),
		linkAssertEvent(stream, p, []string{"evidence", "race-test"}, q, []string{"hypothesis"}, "supports", 3, at),
		linkRenameEvent(stream, p, []string{"evidence", "race-test"}, "race-detector", 4, at.Add(time.Second)),
	}
	later := at.Add(time.Hour)
	ref := Ref{Kind: KindPart, Entity: string(p), Path: []string{"evidence", "race-detector"}}
	links, err := LinksForRef(events, ref, later, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Relation != "supports" || links[0].Direction != "asserted" {
		t.Fatalf("rename survival failed: %+v", links)
	}
	// The historical event retains the old path.
	for _, event := range events {
		if event.Operation == OpAssertLink && event.Target.Entity == string(p) {
			if got, want := strings.Join(event.Target.Path, "/"), "evidence/race-test"; got != want {
				t.Fatalf("historical path = %s, want %s", got, want)
			}
		}
	}
	// Retraction through the new path removes the relation.
	events = append(events, linkRetractEvent(stream, p, []string{"evidence", "race-detector"}, q, []string{"hypothesis"}, "supports", 5, at.Add(2*time.Second)))
	links, err = LinksForRef(events, ref, later, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("retraction through new path failed: %+v", links)
	}
}

func TestMoveSurvival(t *testing.T) {
	at := linkTestAt()
	stream := Ref{Kind: KindTicket, Entity: "ticket:T1"}
	const p, q, m = PartID("part:P"), PartID("part:Q"), PartID("part:M")
	events := []Event{
		linkPartEvent(stream, p, []string{"evidence", "race-test"}, 1, at),
		linkPartEvent(stream, q, []string{"hypothesis"}, 2, at),
		linkPartEvent(stream, m, []string{"verification"}, 3, at),
		linkAssertEvent(stream, p, []string{"evidence", "race-test"}, q, []string{"hypothesis"}, "supports", 4, at),
		linkMoveEvent(stream, p, []string{"evidence", "race-test"}, m, 5, at.Add(time.Second)),
	}
	later := at.Add(time.Hour)
	ref := Ref{Kind: KindPart, Entity: string(p), Path: []string{"verification", "race-test"}}
	links, err := LinksForRef(events, ref, later, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Relation != "supports" {
		t.Fatalf("move survival failed: %+v", links)
	}
}

func TestAliasCollision(t *testing.T) {
	at := linkTestAt()
	stream := Ref{Kind: KindTicket, Entity: "ticket:T1"}
	const p, q, n = PartID("part:P"), PartID("part:Q"), PartID("part:N")
	events := []Event{
		linkPartEvent(stream, p, []string{"evidence", "race-test"}, 1, at),
		linkPartEvent(stream, q, []string{"hypothesis"}, 2, at),
		linkAssertEvent(stream, p, []string{"evidence", "race-test"}, q, []string{"hypothesis"}, "supports", 3, at),
		linkMoveEvent(stream, p, []string{"evidence", "race-test"}, PartID("part:M"), 4, at.Add(time.Second)),
		// New occupant takes the old path.
		linkPartEvent(stream, n, []string{"evidence", "race-test"}, 5, at.Add(2*time.Second)),
	}
	later := at.Add(time.Hour)
	newOccupant := Ref{Kind: KindPart, Entity: string(n), Path: []string{"evidence", "race-test"}}
	links, err := LinksForRef(events, newOccupant, later, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("old-path reuse must not inherit links: %+v", links)
	}
	oldPart := Ref{Kind: KindPart, Entity: string(p), Path: []string{"moved", "race-test"}}
	links, err = LinksForRef(events, oldPart, later, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Relation != "supports" {
		t.Fatalf("old canonical part must keep its link: %+v", links)
	}
}

func TestDiamondInverseNoDuplicateNodes(t *testing.T) {
	at := linkTestAt()
	stream := Ref{Kind: KindTicket, Entity: "ticket:T1"}
	const p, q, r = PartID("part:P"), PartID("part:Q"), PartID("part:R")
	events := []Event{
		linkPartEvent(stream, p, []string{"evidence", "p"}, 1, at),
		linkPartEvent(stream, q, []string{"evidence", "q"}, 2, at),
		linkPartEvent(stream, r, []string{"evidence", "r"}, 3, at),
		linkAssertEvent(stream, p, []string{"evidence", "p"}, q, []string{"evidence", "q"}, "supports", 4, at),
		linkAssertEvent(stream, p, []string{"evidence", "p"}, r, []string{"evidence", "r"}, "contradicts", 5, at),
		linkAssertEvent(stream, r, []string{"evidence", "r"}, q, []string{"evidence", "q"}, "supports", 6, at),
		linkRenameEvent(stream, p, []string{"evidence", "p"}, "p2", 7, at.Add(time.Second)),
		linkRenameEvent(stream, r, []string{"evidence", "r"}, "r2", 8, at.Add(2*time.Second)),
	}
	later := at.Add(time.Hour)
	graph, err := BuildLineageGraph(events, later, later)
	if err != nil {
		t.Fatal(err)
	}
	start := Ref{Kind: KindPart, Entity: string(p), Path: []string{"evidence", "p2"}}
	edges, err := graph.Walk(start, "outgoing", 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("diamond outgoing edges = %d, want 2: %+v", len(edges), edges)
	}
	// Inverse projection uses canonical identity: Q sees both P and R.
	qRef := Ref{Kind: KindPart, Entity: string(q), Path: []string{"evidence", "q"}}
	links, err := LinksForRef(events, qRef, later, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("inverse links = %d, want 2: %+v", len(links), links)
	}
	for _, link := range links {
		if link.Relation != "supported-by" {
			t.Fatalf("unexpected inverse relation: %+v", link)
		}
	}
}
