package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestMultiAssertionCoexistAndTargetedRetract(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	newProjects(t, svc, "a")
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.SetLink(ctx, missis.RequestContext{Actor: "human/local"}, missis.LinkOptions{
		Ref: "project:a/links", Relation: "contains", Target: created.Ref, Add: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SetLink(ctx, missis.RequestContext{Actor: "plugin/x"}, missis.LinkOptions{
		Ref: "project:a/links", Relation: "contains", Target: created.Ref, Add: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	links, err := svc.ShowReferences(ctx, "project:a", missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || len(links[0].Assertions) != 2 {
		t.Fatalf("expected 1 visible relation with 2 assertions: %+v", links)
	}

	// Targeted retraction keeps the relation visible.
	if _, err := svc.SetLink(ctx, missis.RequestContext{Actor: "human/local"}, missis.LinkOptions{
		Ref: "project:a/links", Relation: "contains", Target: created.Ref, Retract: true, Assertion: first.Event,
	}); err != nil {
		t.Fatal(err)
	}
	links, err = svc.ShowReferences(ctx, "project:a", missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || len(links[0].Assertions) != 1 {
		t.Fatalf("relation should stay visible with 1 assertion: %+v", links)
	}

	// Plain retract retracts all remaining assertions.
	if _, err := svc.SetLink(ctx, missis.RequestContext{Actor: "human/local"}, missis.LinkOptions{
		Ref: "project:a/links", Relation: "contains", Target: created.Ref, Retract: true,
	}); err != nil {
		t.Fatal(err)
	}
	links, err = svc.ShowReferences(ctx, "project:a", missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("relation should be hidden after retracting all assertions: %+v", links)
	}
	_ = second
}

func TestTargetedRetractRejectsInactiveAssertion(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	newProjects(t, svc, "a")
	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.SetLink(ctx, missis.RequestContext{}, missis.LinkOptions{
		Ref: "project:a/links", Relation: "contains", Target: created.Ref, Add: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, missis.RequestContext{}, missis.LinkOptions{
		Ref: "project:a/links", Relation: "contains", Target: created.Ref, Retract: true, Assertion: first.Event,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, missis.RequestContext{}, missis.LinkOptions{
		Ref: "project:a/links", Relation: "contains", Target: created.Ref, Retract: true, Assertion: first.Event,
	}); err == nil || !strings.Contains(err.Error(), "re-read and retry") {
		t.Fatalf("retracting an already-inactive assertion must conflict, got %v", err)
	}
}

func TestHasHomeReassertionAllowedForSameProject(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	newProjects(t, svc, "a", "b")
	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "Homed", Project: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, missis.RequestContext{Actor: "plugin/x"}, missis.LinkOptions{
		Ref: created.Ref + "/links", Relation: "has-home", Target: "project:a", Add: true,
	}); err != nil {
		t.Fatalf("same-project has-home re-assertion should be allowed: %v", err)
	}
	if _, err := svc.SetLink(ctx, missis.RequestContext{}, missis.LinkOptions{
		Ref: created.Ref + "/links", Relation: "has-home", Target: "project:b", Add: true,
	}); err == nil || !strings.Contains(err.Error(), "already has a home project") {
		t.Fatalf("different-project has-home must be rejected, got %v", err)
	}
}

func TestMoveLinkRetractsAllAssertions(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	newProjects(t, svc, "a", "b")
	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"human/local", "plugin/x"} {
		if _, err := svc.SetLink(ctx, missis.RequestContext{Actor: actor}, missis.LinkOptions{
			Ref: "project:a/links", Relation: "contains", Target: created.Ref, Add: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.MoveLink(ctx, missis.RequestContext{}, missis.MoveLinkOptions{
		Relation: "contains", From: "project:a", To: "project:b", Target: created.Ref, Reason: "reorg",
	}); err != nil {
		t.Fatal(err)
	}
	viewA, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Project: "a", EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	viewB, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Project: "b", EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewA) != 0 || len(viewB) != 1 {
		t.Fatalf("after move: a=%d b=%d, want 0 and 1", len(viewA), len(viewB))
	}
	links, err := svc.ShowReferences(ctx, "project:a", missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		if link.Relation == "contains" && link.Direction == "asserted" && strings.Contains(link.To, created.Ref) {
			t.Fatalf("old origin should have no active assertions after move: %+v", links)
		}
	}
}
