package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestNewTicketWithHomeAssertsHasHome(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Homed", Project: "safedesign"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Project == nil || *created.Project != "safedesign" {
		t.Fatalf("project = %v, want safedesign", created.Project)
	}
	events, err := svc.LoadLinkEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.Ref{Kind: model.KindTicket, Entity: created.ID}
	views, err := model.LinksForRef(events, ref, now, now)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range views {
		if v.Relation == "has-home" && v.Direction == "asserted" && v.To.Kind == model.KindProject && v.To.Entity == "safedesign" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected asserted has-home link from ticket, got %+v", views)
	}
	filtered, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Project: "safedesign", EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Ref != created.Ref {
		t.Fatalf("project view = %+v, want one ticket %s", filtered, created.Ref)
	}
}

func TestNewTicketMissingProjectFailsWithGuidance(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	_, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "Orphan", Project: "nope"})
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	if !strings.Contains(err.Error(), "missis new --kind project --id nope") {
		t.Fatalf("error must include actionable guidance, got: %v", err)
	}
}

func TestSecondHomeRejected(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	for _, p := range []string{"safedesign", "eng"} {
		if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: p, Title: p}); err != nil {
			t.Fatal(err)
		}
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Homed", Project: "safedesign"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "has-home", Target: "project:eng", Add: true})
	if err == nil {
		t.Fatal("expected second has-home to be rejected")
	}
	if !strings.Contains(err.Error(), "already has a home project") {
		t.Fatalf("error must name the existing assertion, got: %v", err)
	}
}

func TestHasHomeEndpointKinds(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "group", ID: "eng", Title: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "project:safedesign/links", Relation: "has-home", Target: created.Ref, Add: true}); err == nil {
		t.Fatal("has-home from a project must be rejected")
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "has-home", Target: "group:eng", Add: true}); err == nil {
		t.Fatal("has-home to a group must be rejected")
	}
}

func TestHomeRetractionWarnsAndLeavesProjectView(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Homed", Project: "safedesign"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "has-home", Target: "project:safedesign", Retract: true, Reason: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Warning, "zero-home") {
		t.Fatalf("expected zero-home warning, got %q", res.Warning)
	}
	filtered, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Project: "safedesign", EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 0 {
		t.Fatalf("project view after retraction = %+v, want empty", filtered)
	}
}
