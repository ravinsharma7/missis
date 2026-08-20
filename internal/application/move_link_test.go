package application

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func newProjects(t *testing.T, svc *Service, ids ...string) {
	t.Helper()
	ctx := context.Background()
	for _, id := range ids {
		if _, err := svc.NewEntity(ctx, missis.RequestContext{}, missis.EntityOptions{Kind: "project", ID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMoveHomeAtomic(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	newProjects(t, svc, "a", "b")
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Homed", Project: "a"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "has-home", From: "project:a", To: "project:b", Target: created.Ref, Reason: "reorg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Warning != "" {
		t.Fatalf("move must not warn about zero-home: %q", res.Warning)
	}
	if value, ok := res.Value.(string); !ok || !strings.Contains(value, "has-home:project:a->project:b") {
		t.Fatalf("result value = %#v", res.Value)
	}
	viewA, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"a"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewA) != 0 {
		t.Fatalf("project a should be empty after move: %+v", viewA)
	}
	viewB, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"b"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewB) != 1 || viewB[0].Ref != created.Ref {
		t.Fatalf("project b should contain the ticket: %+v", viewB)
	}
	links, err := svc.LoadLinkEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	views, err := model.LinksForRef(links, model.Ref{Kind: model.KindTicket, Entity: created.ID}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	toA, toB := 0, 0
	for _, view := range views {
		if view.Relation != "has-home" || view.Direction != "asserted" {
			continue
		}
		switch view.To.Entity {
		case "a":
			toA++
		case "b":
			toB++
		}
	}
	if toA != 0 || toB != 1 {
		t.Fatalf("home assertions after move: a=%d b=%d", toA, toB)
	}
}

func TestMoveLinkValidation(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	newProjects(t, svc, "a", "b")
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "T", Project: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "has-home", From: "project:a", To: "project:a", Target: created.Ref,
	}); err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("same source/destination must be rejected, got %v", err)
	}
	if _, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "has-home", From: "project:a", To: "project:nope", Target: created.Ref,
	}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing destination must be rejected, got %v", err)
	}
	if _, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "supports", From: "project:a", To: "project:b", Target: created.Ref,
	}); err == nil || !strings.Contains(err.Error(), "membership relations") {
		t.Fatalf("non-membership relation must be rejected, got %v", err)
	}
	if _, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "contains", From: "project:a", To: "project:b", Target: created.Ref,
	}); err == nil || !strings.Contains(err.Error(), "no active contains assertion") {
		t.Fatalf("move without active assertion must be rejected, got %v", err)
	}
}

func TestMoveLinkPreconditionConflict(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	newProjects(t, svc, "a", "b")
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Homed", Project: "a"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "has-home", From: "project:a", To: "project:b", Target: created.Ref, Reason: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "has-home", From: "project:b", To: "project:a", Target: created.Ref, IfCurrent: first.Event, Reason: "2",
	}); err != nil {
		t.Fatalf("move with matching precondition should pass: %v", err)
	}
	if _, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "has-home", From: "project:a", To: "project:b", Target: created.Ref, IfCurrent: first.Event, Reason: "3",
	}); err == nil || !strings.Contains(err.Error(), "re-read and retry") {
		t.Fatalf("stale precondition must conflict, got %v", err)
	}
}

func TestMoveLinkCrossStreamContains(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	newProjects(t, svc, "a", "b")
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "project:a/links", Relation: "contains", Target: created.Ref, Add: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MoveLink(ctx, req, missis.MoveLinkOptions{
		Relation: "contains", From: "project:a", To: "project:b", Target: created.Ref, Reason: "reorg",
	}); err != nil {
		t.Fatal(err)
	}
	viewA, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"a"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewA) != 0 {
		t.Fatalf("project a should be empty after contains move: %+v", viewA)
	}
	viewB, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"b"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewB) != 1 || viewB[0].Ref != created.Ref {
		t.Fatalf("project b should contain the ticket: %+v", viewB)
	}
}

func TestConcurrentMoveHomeExactlyOneWinner(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	projects := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	newProjects(t, svc, projects...)
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Homed", Project: "a"})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([]error, len(projects)-1)
	for i, p := range projects[1:] {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			_, err := svc.MoveLink(context.Background(), missis.RequestContext{}, missis.MoveLinkOptions{
				Relation: "has-home", From: "project:a", To: "project:" + p, Target: created.Ref, Reason: "race",
			})
			results[i] = err
		}(i, p)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one winner, got %d (results: %v)", successes, results)
	}

	links, err := svc.LoadLinkEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	views, err := model.LinksForRef(links, model.Ref{Kind: model.KindTicket, Entity: created.ID}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	homeTargets := 0
	for _, view := range views {
		if view.Relation == "has-home" && view.Direction == "asserted" {
			homeTargets++
		}
	}
	if homeTargets != 1 {
		t.Fatalf("final home assertions = %d, want exactly 1", homeTargets)
	}
}

func TestConcurrentMoveLinkAcrossScopes(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	newProjects(t, svc, "a", "b", "c")
	for _, id := range []string{"g1", "g2"} {
		if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "group", ID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
	}

	// Three independent tickets: one home move, one contains move, one
	// governs move, run concurrently across project/group boundaries.
	homeTicket, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Home", Project: "a"})
	if err != nil {
		t.Fatal(err)
	}
	containsTicket, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Contains"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "project:a/links", Relation: "contains", Target: containsTicket.Ref, Add: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "group:g1/links", Relation: "governs", Target: "project:c", Add: true}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 3)
	ops := []func() error{
		func() error {
			_, err := svc.MoveLink(context.Background(), missis.RequestContext{}, missis.MoveLinkOptions{
				Relation: "has-home", From: "project:a", To: "project:b", Target: homeTicket.Ref, Reason: "race",
			})
			return err
		},
		func() error {
			_, err := svc.MoveLink(context.Background(), missis.RequestContext{}, missis.MoveLinkOptions{
				Relation: "contains", From: "project:a", To: "project:c", Target: containsTicket.Ref, Reason: "race",
			})
			return err
		},
		func() error {
			_, err := svc.MoveLink(context.Background(), missis.RequestContext{}, missis.MoveLinkOptions{
				Relation: "governs", From: "group:g1", To: "group:g2", Target: "project:c", Reason: "race",
			})
			return err
		},
	}
	for i, op := range ops {
		wg.Add(1)
		go func(i int, op func() error) {
			defer wg.Done()
			errs[i] = op()
		}(i, op)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("independent concurrent move %d failed: %v", i, err)
		}
	}

	// Home moved a -> b.
	links, err := svc.LoadLinkEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	views, err := model.LinksForRef(links, model.Ref{Kind: model.KindTicket, Entity: homeTicket.ID}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	homeToB := false
	for _, view := range views {
		if view.Relation == "has-home" && view.Direction == "asserted" {
			homeToB = view.To.Entity == "b"
		}
	}
	if !homeToB {
		t.Fatalf("home should be b after concurrent move: %+v", views)
	}

	// Contains moved a -> c.
	viewA, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"a"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	viewC, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"c"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewA) != 0 || len(viewC) != 1 {
		t.Fatalf("contains move: a=%d c=%d", len(viewA), len(viewC))
	}

	// Governs moved g1 -> g2; group g2 sees project c tickets, g1 does not.
	viewG1, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Groups: []string{"g1"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	viewG2, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Groups: []string{"g2"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewG1) != 0 || len(viewG2) != 1 {
		t.Fatalf("governs move: g1=%d g2=%d", len(viewG1), len(viewG2))
	}
}
