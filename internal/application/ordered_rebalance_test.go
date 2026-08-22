package application

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestDenseMovesRebalanceAtomicallyAndSurviveReopen(t *testing.T) {
	// covers N105
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "missis.db")
	svc, err := OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	created, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "dense order"})
	if err != nil {
		t.Fatal(err)
	}

	parentID := model.PartID("part:ordered-parent")
	stream := model.Ref{Kind: model.KindTicket, Entity: created.ID}
	parentRef := model.Ref{Kind: model.KindPart, Entity: string(parentID)}
	events := []model.Event{{
		ID: "event:ordered-parent", Stream: stream, Operation: model.OpCreatePart,
		Target: model.Ref{Kind: model.KindPart, Entity: string(parentID), Path: []string{"evidence"}},
		Value:  model.Value{Kind: model.ValueKindMarkdown, Text: "evidence"}, Actor: model.ActorRef{Kind: "test", ID: "test"},
	}}
	const childCount = 28
	for i := 0; i < childCount; i++ {
		id := model.PartID("part:ordered-child-" + string(rune('a'+i)))
		path := []string{"evidence", "child-" + string(rune('a'+i))}
		events = append(events, model.Event{
			ID: model.EventID("event:ordered-child-" + string(rune('a'+i))), Stream: stream, Operation: model.OpCreatePart,
			Target: model.Ref{Kind: model.KindPart, Entity: string(id), Path: path},
			Value:  model.Value{Kind: model.ValueKindMarkdown, Text: path[1], Ref: &parentRef, OrderKey: model.OrderKeyForIndex(i)},
			Actor:  model.ActorRef{Kind: "test", ID: "test"},
		})
	}
	if _, err := client.AppendBatch(ctx, events, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := client.LoadTicketEvents(ctx, model.TicketID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	beforeHashes := make(map[string]string, len(beforeEvents))
	for _, event := range beforeEvents {
		beforeHashes[string(event.ID)] = event.Hash
	}

	for i := 2; i < childCount; i++ {
		id := "part:ordered-child-" + string(rune('a'+i))
		if _, err := client.Set(ctx, missis.RequestContext{Actor: "test"}, missis.MovePart{
			Target: "part:" + id, Parent: "part:" + string(parentID), Before: "part:part:ordered-child-b", Reason: "dense insertion test",
		}); err != nil {
			t.Fatalf("move %s: %v", id, err)
		}
	}

	projection, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var ordered []string
	for _, path := range projection.PartOrder {
		if strings.HasPrefix(path, "evidence/child-") {
			ordered = append(ordered, strings.TrimPrefix(path, "evidence/"))
		}
	}
	if len(ordered) != childCount || ordered[0] != "child-a" || ordered[len(ordered)-1] != "child-b" {
		t.Fatalf("dense order = %v", ordered)
	}
	for i := 1; i < len(ordered)-1; i++ {
		if ordered[i] != "child-"+string(rune('a'+i+1)) {
			t.Fatalf("dense order at %d = %v", i, ordered)
		}
	}

	finalEvents, err := client.LoadTicketEvents(ctx, model.TicketID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range finalEvents {
		if hash, ok := beforeHashes[string(event.ID)]; ok && hash != event.Hash {
			t.Fatalf("historical event hash changed for %s", event.ID)
		}
	}
	moveEvents := 0
	for _, event := range finalEvents {
		if event.Operation == model.OpMovePart && event.Reason == "ordered containment rebalance" {
			moveEvents++
		}
	}
	if moveEvents == 0 {
		t.Fatal("dense insertion never emitted a rebalance event batch")
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	reopenedClient := missis.NewClient(reopened)
	defer reopenedClient.Close()
	if err := reopenedClient.RebuildProjection(ctx); err != nil {
		t.Fatal(err)
	}
	reopenedProjection, err := reopenedClient.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(reopenedProjection.PartOrder, "\n") != strings.Join(projection.PartOrder, "\n") {
		t.Fatalf("reopened order changed: %v != %v", reopenedProjection.PartOrder, projection.PartOrder)
	}
}
