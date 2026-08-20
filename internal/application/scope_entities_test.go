package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestListEntities(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	for _, e := range []struct{ kind, id, title string }{
		{"project", "safedesign", "SafeDesign"},
		{"project", "eng", "Engineering"},
		{"group", "security", "Security"},
	} {
		if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: e.kind, ID: e.id, Title: e.title}); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := svc.ListEntities(ctx, model.KindProject, missis.ListFilter{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %+v", projects)
	}
	if projects[0].Ref != "project:eng" || projects[1].Ref != "project:safedesign" {
		t.Fatalf("projects must be in canonical order: %+v", projects)
	}
	if projects[1].Title != "SafeDesign" {
		t.Fatalf("project title = %q, want SafeDesign", projects[1].Title)
	}
	groups, err := svc.ListEntities(ctx, model.KindGroup, missis.ListFilter{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Ref != "group:security" {
		t.Fatalf("groups = %+v", groups)
	}
	searched, err := svc.ListEntities(ctx, model.KindProject, missis.ListFilter{Query: "Safe", EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(searched) != 1 || searched[0].Ref != "project:safedesign" {
		t.Fatalf("search = %+v", searched)
	}
	if _, err := svc.ListEntities(ctx, model.Kind("bogus"), missis.ListFilter{}); err == nil {
		t.Fatal("invalid kind must be rejected")
	}
}

func TestGroupViewDirectContains(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "group", ID: "eng", Title: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "group:eng/links", Relation: "contains", Target: created.Ref, Add: true}); err != nil {
		t.Fatal(err)
	}
	filtered, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Groups: []string{"eng"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Ref != created.Ref {
		t.Fatalf("group view = %+v", filtered)
	}
}

func TestScopeEntityHistory(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "proj", Title: "Proj"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: "project:proj/notes", Value: "hello", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	all, err := svc.ShowHistory(ctx, "project:proj", missis.HistoryOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("scope history = %+v", all)
	}
	part, err := svc.ShowHistory(ctx, "project:proj/notes", missis.HistoryOptions{EffectiveAt: now, KnownAt: now, PartPath: []string{"notes"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(part) != 2 || part[1].Operation != "set-value" {
		t.Fatalf("scope part history = %+v", part)
	}
}

func TestShowEntityBoundaries(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "proj", Title: "Proj"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ShowEntity(ctx, "project:proj", missis.ShowOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ShowEntity(ctx, "#1", missis.ShowOptions{}); err == nil {
		t.Fatal("ShowEntity must reject ticket refs")
	}
	if _, err := svc.ShowTicket(ctx, "project:proj", missis.ShowOptions{}); err == nil {
		t.Fatal("ShowTicket must reject scope refs")
	}
}

func TestLinkTargetResolution(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "p1", Title: "P1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "project:p1/links", Relation: "contains", Target: "project:nope", Add: true}); err == nil {
		t.Fatal("link to nonexistent project must be rejected")
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "project:p1/links", Relation: "governs", Target: "group:nope", Add: true}); err == nil {
		t.Fatal("link to nonexistent group must be rejected")
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "project:p1/links", Relation: "contains", Target: "ticket:01M0NOPE", Add: true}); err == nil {
		t.Fatal("link to nonexistent ticket must be rejected")
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: "project:p1/links", Relation: "contains", Target: "project:nope", Add: true}); err == nil || !strings.Contains(err.Error(), "missis new --kind project --id nope") {
		t.Fatalf("expected actionable guidance, got: %v", err)
	}
}
