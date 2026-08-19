package application

import (
	"context"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestShowReferencesResolvesCurrentPathAfterRename(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Linked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: created.Ref + "/problem", Value: "body", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: created.Ref + "/evidence", Value: "run", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/problem/links", Relation: "supports", Target: created.Ref + "/evidence", Add: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.RenamePart{Target: created.Ref + "/evidence", Name: "evidence-2"}); err != nil {
		t.Fatal(err)
	}

	links, err := svc.ShowReferences(ctx, created.Ref+"/problem", missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].To != "evidence-2" {
		t.Fatalf("current path not resolved after rename: %+v", links)
	}

	// Historical link events retain the old path.
	linkEvents, err := svc.LoadLinkEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, event := range linkEvents {
		if event.Operation == model.OpAssertLink && event.Value.Ref != nil && event.Value.Ref.Entity == "" {
			continue
		}
		if event.Operation == model.OpAssertLink && strings.Join(event.Value.Ref.Path, "/") == "evidence" {
			found = true
		}
	}
	if !found {
		t.Fatal("historical link event must retain the old path")
	}

	// Projection rebuild keeps canonical link resolution intact.
	if err := svc.RebuildProjection(ctx); err != nil {
		t.Fatal(err)
	}
	links, err = svc.ShowReferences(ctx, created.Ref+"/problem", missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].To != "evidence-2" {
		t.Fatalf("current path lost after projection rebuild: %+v", links)
	}
}
