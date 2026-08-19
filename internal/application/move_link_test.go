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
	viewA, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Project: "a", EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewA) != 0 {
		t.Fatalf("project a should be empty after move: %+v", viewA)
	}
	viewB, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Project: "b", EffectiveAt: now, KnownAt: now})
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
	viewA, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Project: "a", EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(viewA) != 0 {
		t.Fatalf("project a should be empty after contains move: %+v", viewA)
	}
	viewB, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Project: "b", EffectiveAt: now, KnownAt: now})
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
