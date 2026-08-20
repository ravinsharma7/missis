package application

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestApplyLinkBatchIsAtomicAndSkipsActiveDuplicates(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	newProjects(t, svc, "app")
	if _, err := svc.NewEntity(ctx, missis.RequestContext{}, missis.EntityOptions{Kind: "group", ID: "eng", Title: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	ticket, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "Batch"})
	if err != nil {
		t.Fatal(err)
	}
	items := missis.LinkBatchOptions{Items: []missis.LinkBatchItem{
		{Source: ticket.Ref, Relation: "has-home", Target: "project:app"},
		{Source: "group:eng", Relation: "contains", Target: ticket.Ref},
	}}
	result, err := svc.ApplyLinkBatch(ctx, missis.RequestContext{}, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 2 || len(result.Skipped) != 0 {
		t.Fatalf("first batch result = %+v", result)
	}

	repeated, err := svc.ApplyLinkBatch(ctx, missis.RequestContext{}, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.Added) != 0 || len(repeated.Skipped) != 2 {
		t.Fatalf("repeated batch result = %+v", repeated)
	}

	bad, err := svc.ApplyLinkBatch(ctx, missis.RequestContext{}, missis.LinkBatchOptions{Items: []missis.LinkBatchItem{
		{Source: "group:eng", Relation: "contains", Target: ticket.Ref},
		{Source: "group:eng", Relation: "contains", Target: "ticket:missing"},
	}})
	if err == nil || bad.Added != nil {
		t.Fatalf("invalid batch should fail before writing: result=%+v err=%v", bad, err)
	}
	links, err := svc.ShowReferences(ctx, "group:eng", missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || len(links[0].Assertions) != 1 {
		t.Fatalf("invalid batch changed existing links: %+v", links)
	}

	if _, err := svc.SetLink(ctx, missis.RequestContext{}, missis.LinkOptions{
		Ref: "group:eng/links", Relation: "contains", Target: ticket.Ref, Retract: true,
	}); err != nil {
		t.Fatal(err)
	}
	readded, err := svc.ApplyLinkBatch(ctx, missis.RequestContext{}, missis.LinkBatchOptions{Items: []missis.LinkBatchItem{
		{Source: "group:eng", Relation: "contains", Target: ticket.Ref},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(readded.Added) != 1 || len(readded.Skipped) != 0 {
		t.Fatalf("re-add result = %+v", readded)
	}
}

func TestApplyLinkBatchAbsencePreconditionSerializesDuplicateWriters(t *testing.T) {
	now := fixedNow()
	dir := t.TempDir()
	path := filepath.Join(dir, "missis.db")
	svc1, err := OpenPathWithClock(path, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	defer svc1.Close()
	newProjects(t, svc1, "app")
	ticket, err := svc1.NewTicket(context.Background(), missis.RequestContext{}, missis.NewTicketOptions{Title: "Concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	svc2, err := OpenPathWithClock(path, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	defer svc2.Close()
	batch := missis.LinkBatchOptions{Items: []missis.LinkBatchItem{{
		Source: "project:app", Relation: "contains", Target: ticket.Ref,
	}}}
	results := make([]missis.LinkBatchResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0], errs[0] = svc1.ApplyLinkBatch(context.Background(), missis.RequestContext{Actor: "one"}, batch)
	}()
	go func() {
		defer wg.Done()
		results[1], errs[1] = svc2.ApplyLinkBatch(context.Background(), missis.RequestContext{Actor: "two"}, batch)
	}()
	wg.Wait()
	added, skipped, conflicts := 0, 0, 0
	for i := range results {
		if errs[i] == nil {
			added += len(results[i].Added)
			skipped += len(results[i].Skipped)
			continue
		}
		conflicts++
	}
	if added != 1 || skipped+conflicts != 1 {
		t.Fatalf("concurrent results: results=%+v errs=%+v", results, errs)
	}
	links, err := svc1.ShowReferences(context.Background(), "project:app", missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || len(links[0].Assertions) != 1 {
		t.Fatalf("concurrent writers produced duplicate assertions: %+v", links)
	}
}

func TestApplyLinkBatchConcurrentIndependentMemberships(t *testing.T) {
	now := fixedNow()
	dir := t.TempDir()
	path := filepath.Join(dir, "missis.db")
	svc1, err := OpenPathWithClock(path, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	defer svc1.Close()
	for _, id := range []string{"one", "two"} {
		if _, err := svc1.NewEntity(context.Background(), missis.RequestContext{}, missis.EntityOptions{Kind: "group", ID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	ticket, err := svc1.NewTicket(context.Background(), missis.RequestContext{}, missis.NewTicketOptions{Title: "Concurrent memberships"})
	if err != nil {
		t.Fatal(err)
	}
	svc2, err := OpenPathWithClock(path, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	defer svc2.Close()

	batches := []missis.LinkBatchOptions{
		{Items: []missis.LinkBatchItem{{Source: "group:one", Relation: model.RelationContains, Target: ticket.Ref}}},
		{Items: []missis.LinkBatchItem{{Source: "group:two", Relation: model.RelationContains, Target: ticket.Ref}}},
	}
	results := make([]missis.LinkBatchResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, svc := range []*Service{svc1, svc2} {
		wg.Add(1)
		go func(i int, svc *Service) {
			defer wg.Done()
			results[i], errs[i] = svc.ApplyLinkBatch(context.Background(), missis.RequestContext{Actor: "concurrent"}, batches[i])
		}(i, svc)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("independent batch %d failed: %v", i, err)
		}
		if len(results[i].Added) != 1 {
			t.Fatalf("independent batch %d result = %+v", i, results[i])
		}
	}
	for _, group := range []string{"group:one", "group:two"} {
		links, err := svc1.ShowReferences(context.Background(), group, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(links) != 1 || len(links[0].Assertions) != 1 {
			t.Fatalf("group %s membership = %+v", group, links)
		}
	}
}
