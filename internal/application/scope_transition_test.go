package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestJoinScopeAndLeaveScope(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "group", ID: "eng", Title: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.JoinScope(ctx, req, missis.ScopeOptions{Entity: created.Ref, Scope: "group:eng", Reason: "join"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.JoinScope(ctx, missis.RequestContext{Actor: "plugin/x"}, missis.ScopeOptions{Entity: created.Ref, Scope: "group:eng"}); err != nil {
		t.Fatalf("second join should be allowed (evidence semantics): %v", err)
	}

	links, err := svc.ShowReferences(ctx, created.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	memberOf := 0
	for _, link := range links {
		if link.Relation == "member-of" && link.Direction == "asserted" {
			memberOf++
			if len(link.Assertions) != 2 {
				t.Fatalf("expected 2 membership assertions: %+v", link.Assertions)
			}
		}
	}
	if memberOf != 1 {
		t.Fatalf("member-of relations = %d, want 1: %+v", memberOf, links)
	}

	// Targeted leave keeps membership while another assertion remains.
	if _, err := svc.LeaveScope(ctx, req, missis.ScopeOptions{Entity: created.Ref, Scope: "group:eng", Assertion: first.Event}); err != nil {
		t.Fatal(err)
	}
	links, err = svc.ShowReferences(ctx, created.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		if link.Relation == "member-of" && link.Direction == "asserted" {
			if len(link.Assertions) != 1 {
				t.Fatalf("targeted leave should keep 1 assertion: %+v", link.Assertions)
			}
		}
	}

	// Plain leave retracts all remaining assertions.
	if _, err := svc.LeaveScope(ctx, req, missis.ScopeOptions{Entity: created.Ref, Scope: "group:eng"}); err != nil {
		t.Fatal(err)
	}
	links, err = svc.ShowReferences(ctx, created.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		if link.Relation == "member-of" && link.Direction == "asserted" {
			t.Fatalf("membership should be hidden after leaving: %+v", links)
		}
	}
}

func TestScopeTransitionValidation(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	if _, err := svc.NewEntity(ctx, missis.RequestContext{}, missis.EntityOptions{Kind: "group", ID: "eng", Title: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.JoinScope(ctx, missis.RequestContext{}, missis.ScopeOptions{Entity: created.Ref, Scope: created.Ref}); err == nil || !strings.Contains(err.Error(), "scope must be a project or group") {
		t.Fatalf("scope must be project/group, got %v", err)
	}
	if _, err := svc.JoinScope(ctx, missis.RequestContext{}, missis.ScopeOptions{Entity: created.Ref, Scope: "group:nope"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing scope must be rejected, got %v", err)
	}
	if _, err := svc.LeaveScope(ctx, missis.RequestContext{}, missis.ScopeOptions{Entity: created.Ref, Scope: "group:eng"}); err == nil || !strings.Contains(err.Error(), "nothing to leave") {
		t.Fatalf("leaving without membership must be rejected, got %v", err)
	}
}
