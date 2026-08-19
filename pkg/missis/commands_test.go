package missis_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func testClient(t *testing.T) *missis.Client {
	t.Helper()
	svc, err := application.OpenPath(filepath.Join(t.TempDir(), "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return missis.NewClient(svc)
}

func req() missis.RequestContext {
	return missis.RequestContext{Actor: "test"}
}

func TestNewTicketShowAndList(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	first, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "First"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Ref != "#1" || first.Status != "open" || first.ID == "" {
		t.Fatalf("unexpected new result: %+v", first)
	}
	second, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "Second", Types: []string{"bug"}, Tags: []string{"sdk"}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Ref != "#2" {
		t.Fatalf("second ref = %v", second.Ref)
	}

	proj, err := client.ShowTicket(ctx, first.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Title != "First" || proj.Status != "open" {
		t.Fatalf("projection = %+v", proj)
	}
	if _, ok := proj.Parts["title"]; !ok {
		t.Fatalf("title part missing: %+v", proj.Parts)
	}

	summaries, err := client.ListTicketSummaries(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
}

func TestSetPartVariants(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	created, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "Variants"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Set(ctx, req(), missis.SetValue{Target: created.Ref + "/problem", Value: "body", Kind: model.ValueKindText}); err != nil {
		t.Fatalf("set problem: %v", err)
	}
	if _, err := client.Set(ctx, req(), missis.AddValue{Target: created.Ref + "/type", Value: "sdk"}); err != nil {
		t.Fatalf("add type: %v", err)
	}
	if _, err := client.Set(ctx, req(), missis.SetValue{Target: created.Ref + "/status", Value: "blocked", Kind: model.ValueKindStatus}); err == nil {
		t.Fatal("expected blocked status to require reason")
	}
	if _, err := client.Set(ctx, req(), missis.SetValue{Target: created.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatalf("set doing: %v", err)
	}
	if _, err := client.Set(ctx, req(), missis.SetValue{Target: created.Ref + "/notes", Value: "scratch", Kind: model.ValueKindText}); err != nil {
		t.Fatalf("set notes: %v", err)
	}
	if _, err := client.Set(ctx, req(), missis.RetractValue{Target: created.Ref + "/notes", Reason: "moved"}); err != nil {
		t.Fatalf("retract notes: %v", err)
	}

	proj, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Status != "doing" {
		t.Fatalf("status = %q", proj.Status)
	}
	if part, ok := proj.Parts["notes"]; ok && part.Value != nil {
		t.Fatalf("retracted notes still has value: %+v", part)
	}
}

func TestSetPartPreconditionConflict(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	created, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "Conflict"})
	if err != nil {
		t.Fatal(err)
	}
	history, err := client.ShowHistory(ctx, created.Ref+"/status", missis.HistoryOptions{PartPath: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("status history = %d events", len(history))
	}
	oldAlias := history[0].Alias
	if _, err := client.Set(ctx, req(), missis.SetValue{Target: created.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatal(err)
	}
	conflictReq := req()
	conflictReq.IfCurrent = oldAlias
	if _, err := client.Set(ctx, conflictReq, missis.SetValue{Target: created.Ref + "/status", Value: "done", Kind: model.ValueKindStatus}); err == nil {
		t.Fatal("expected optimistic concurrency conflict")
	}
}

func TestLinksReferencesLineage(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	first, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetLink(ctx, req(), missis.LinkOptions{Ref: first.Ref + "/links", Relation: "blocked-by", Target: second.Ref, Add: true}); err != nil {
		t.Fatal(err)
	}
	refs, err := client.ShowReferences(ctx, first.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Relation != "blocked-by" {
		t.Fatalf("references = %+v", refs)
	}
	inverse, err := client.ShowReferences(ctx, second.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inverse) != 1 || inverse[0].Relation != "blocks" {
		t.Fatalf("inverse references = %+v", inverse)
	}
	edges, err := client.ShowLineage(ctx, first.Ref, missis.LineageOptions{Direction: "both", Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("lineage edges = %+v", edges)
	}
	if _, err := client.SetLink(ctx, req(), missis.LinkOptions{Ref: first.Ref + "/links", Relation: "blocked-by", Target: second.Ref, Retract: true}); err != nil {
		t.Fatal(err)
	}
	after, err := client.ShowReferences(ctx, first.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("expected no links after retract: %+v", after)
	}
}

func TestMarkdownImportAndReimport(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	content := "# Root\n\n## Problem\n\nThe problem body.\n\n## Evidence\n\nEvidence body.\n"

	created, err := client.ImportMarkdown(ctx, req(), missis.ImportOptions{Content: content, Artifact: "artifact:test.md"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := proj.Parts["problem"]; !ok {
		t.Fatalf("problem part missing: %+v", proj.Parts)
	}

	updated := "# Root\n\n## Problem\n\nUpdated body.\n\n## Evidence\n\nEvidence body.\n"
	result, err := client.ReimportMarkdown(ctx, req(), missis.ImportOptions{Ref: created.Ref, Content: updated, Artifact: "artifact:test.md"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value == 0 {
		t.Fatalf("expected reimport to write changes, got %+v", result)
	}
	proj2, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := proj2.Parts["problem"].Value; got != "Updated body." {
		t.Fatalf("problem body = %v", got)
	}

	if _, err := client.ReimportMarkdown(ctx, req(), missis.ImportOptions{Ref: created.Ref, Content: updated, Artifact: "artifact:test.md"}); err != nil {
		t.Fatal(err)
	}
	proj3, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(proj3.Parts) != len(proj2.Parts) {
		t.Fatalf("unchanged reimport changed part count: %d != %d", len(proj3.Parts), len(proj2.Parts))
	}

	missing := "# Root\n\n## Problem\n\nUpdated body.\n"
	if _, err := client.ReimportMarkdown(ctx, req(), missis.ImportOptions{Ref: created.Ref, Content: missing, Artifact: "artifact:test.md"}); err == nil {
		t.Fatal("expected missing-part reimport to fail")
	}
}

func TestSearchAndFilters(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	first, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "retry race"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "unrelated"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Set(ctx, req(), missis.SetValue{Target: first.Ref + "/problem", Value: "worker retry after shutdown", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Set(ctx, req(), missis.SetValue{Target: first.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatal(err)
	}

	results, err := client.Search(ctx, missis.SearchOptions{Query: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search results = %+v", results)
	}
	doing, err := client.Search(ctx, missis.SearchOptions{Status: "doing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(doing) != 1 {
		t.Fatalf("status filter results = %+v", doing)
	}
	none, err := client.Search(ctx, missis.SearchOptions{Query: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no results, got %+v", none)
	}
}

func TestProjectGroupFiltering(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	if _, err := client.NewEntity(ctx, req(), missis.EntityOptions{Kind: "project", ID: "proj", Title: "Project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewEntity(ctx, req(), missis.EntityOptions{Kind: "group", ID: "eng", Title: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	ticket, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "Scoped"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetLink(ctx, req(), missis.LinkOptions{Ref: "project:proj/links", Relation: "contains", Target: ticket.Ref, Add: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetLink(ctx, req(), missis.LinkOptions{Ref: "group:eng/links", Relation: "contains", Target: "project:proj", Add: true}); err != nil {
		t.Fatal(err)
	}
	byProject, err := client.ListTicketsFiltered(ctx, missis.ListFilter{Project: "proj"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byProject) != 1 {
		t.Fatalf("project tickets = %+v", byProject)
	}
	byGroup, err := client.ListTicketsFiltered(ctx, missis.ListFilter{Group: "eng"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byGroup) != 1 {
		t.Fatalf("group tickets = %+v", byGroup)
	}
	if _, err := client.NewEntity(ctx, req(), missis.EntityOptions{Kind: "project", ID: "proj", Title: "Duplicate"}); err == nil {
		t.Fatal("expected duplicate project error")
	}
}

func TestBitemporalShow(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	created, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "Bitemporal"})
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	futureReq := req()
	futureReq.EffectiveAt = future
	if _, err := client.Set(ctx, futureReq, missis.SetValue{Target: created.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatal(err)
	}
	now, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if now.Status != "open" {
		t.Fatalf("current status = %q", now.Status)
	}
	later, err := client.ShowTicket(ctx, created.Ref, missis.ShowOptions{EffectiveAt: future, KnownAt: future})
	if err != nil {
		t.Fatal(err)
	}
	if later.Status != "doing" {
		t.Fatalf("future status = %q", later.Status)
	}
}

func TestIdempotency(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	newReq := req()
	newReq.IdempotencyKey = "k-new"
	first, err := client.NewTicket(ctx, newReq, missis.NewTicketOptions{Title: "Idem"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.NewTicket(ctx, newReq, missis.NewTicketOptions{Title: "Idem"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Ref != second.Ref || first.ID != second.ID {
		t.Fatalf("idempotent new mismatch: %+v vs %+v", first, second)
	}
	setReq := req()
	setReq.IdempotencyKey = "k-set"
	setA, err := client.Set(ctx, setReq, missis.SetValue{Target: first.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus})
	if err != nil {
		t.Fatal(err)
	}
	setB, err := client.Set(ctx, setReq, missis.SetValue{Target: first.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus})
	if err != nil {
		t.Fatal(err)
	}
	if setA.Event == "" || setA.Event != setB.Event {
		t.Fatalf("idempotent set mismatch: %+v vs %+v", setA, setB)
	}
}

func TestErrorMessagesSurfaceNotFound(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	if _, err := client.ShowTicket(ctx, "#9999", missis.ShowOptions{}); err == nil {
		t.Fatal("expected not found error")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
