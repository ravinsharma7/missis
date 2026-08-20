package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestImportMarkdownWithHome(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.ImportMarkdown(ctx, req, missis.ImportOptions{
		Title: "Imported", Content: "## body\n\nhello\n", Project: "safedesign",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Project == nil || *created.Project != "safedesign" {
		t.Fatalf("project = %v", created.Project)
	}
	links, err := svc.LoadLinkEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	views, err := model.LinksForRef(links, model.Ref{Kind: model.KindTicket, Entity: created.ID}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	homeCount := 0
	for _, view := range views {
		if view.Relation == "has-home" && view.Direction == "asserted" && view.To.Entity == "safedesign" {
			homeCount++
		}
	}
	if homeCount != 1 {
		t.Fatalf("has-home assertions = %d, want 1: %+v", homeCount, views)
	}
	filtered, err := svc.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"safedesign"}, EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Ref != created.Ref {
		t.Fatalf("project view = %+v", filtered)
	}
}

func TestImportMarkdownMissingProjectGuidance(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	before, err := svc.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ImportMarkdown(ctx, missis.RequestContext{}, missis.ImportOptions{
		Title: "X", Content: "## body\n", Project: "nope",
	})
	if err == nil || !strings.Contains(err.Error(), "missis new --kind project --id nope") {
		t.Fatalf("expected actionable guidance, got %v", err)
	}
	after, err := svc.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("import must be atomic: events before=%d after=%d", before, after)
	}
}
