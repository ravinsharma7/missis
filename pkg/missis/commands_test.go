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

func TestMultiScopeFilteringSDK(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	for _, entity := range []missis.EntityOptions{
		{Kind: "project", ID: "p1", Title: "Project 1"},
		{Kind: "project", ID: "p2", Title: "Project 2"},
		{Kind: "group", ID: "g1", Title: "Group 1"},
		{Kind: "group", ID: "g2", Title: "Group 2"},
	} {
		if _, err := client.NewEntity(ctx, req(), entity); err != nil {
			t.Fatal(err)
		}
	}
	ticketA, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "A", Project: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	ticketB, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "B", Project: "p2"})
	if err != nil {
		t.Fatal(err)
	}
	ticketC, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "C"})
	if err != nil {
		t.Fatal(err)
	}
	ticketD, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "D", Project: "p2"})
	if err != nil {
		t.Fatal(err)
	}
	ticketE, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "E"})
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range []missis.LinkOptions{
		{Ref: "group:g1/links", Relation: "contains", Target: "project:p1", Add: true},
		{Ref: "group:g1/links", Relation: "contains", Target: "project:p2", Add: true},
		{Ref: "group:g2/links", Relation: "contains", Target: "project:p2", Add: true},
		{Ref: "group:g2/links", Relation: "contains", Target: ticketC.Ref, Add: true},
	} {
		if _, err := client.SetLink(ctx, req(), link); err != nil {
			t.Fatal(err)
		}
	}

	refs := func(items []missis.TicketSummary) map[string]bool {
		result := make(map[string]bool, len(items))
		for _, item := range items {
			result[item.Ref] = true
		}
		return result
	}
	assertRefs := func(name string, got []missis.TicketSummary, want ...string) {
		t.Helper()
		gotRefs := refs(got)
		if len(gotRefs) != len(want) {
			t.Fatalf("%s refs = %v, want %v", name, gotRefs, want)
		}
		for _, ref := range want {
			if !gotRefs[ref] {
				t.Fatalf("%s refs = %v, missing %s", name, gotRefs, ref)
			}
		}
	}

	projects, err := client.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"p2", "p1", "p2"}})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("project union", projects, ticketA.Ref, ticketB.Ref, ticketD.Ref)

	groups, err := client.ListTicketsFiltered(ctx, missis.ListFilter{Groups: []string{"g1", "g2"}})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("group union", groups, ticketA.Ref, ticketB.Ref, ticketC.Ref, ticketD.Ref)

	intersection, err := client.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"p2"}, Groups: []string{"g2"}})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("project/group intersection", intersection, ticketB.Ref, ticketD.Ref)

	legacy, err := client.ListTicketsFiltered(ctx, missis.ListFilter{
		Projects: []string{"p1", "", " "},
		Project:  " p2, p1, ",
		Groups:   []string{"g1", ""},
		Group:    "g1,,",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("typed and legacy merge", legacy, ticketA.Ref, ticketB.Ref, ticketD.Ref)

	search, err := client.Search(ctx, missis.SearchOptions{Projects: []string{"p1"}, Groups: []string{"g1"}})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("typed search", search, ticketA.Ref)

	all, err := client.ListTicketsFiltered(ctx, missis.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("empty scope", all, ticketA.Ref, ticketB.Ref, ticketC.Ref, ticketD.Ref, ticketE.Ref)

	unscoped, err := client.ListTicketsFiltered(ctx, missis.ListFilter{Unscoped: true})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("unscoped", unscoped, ticketE.Ref)

	searchUnscoped, err := client.Search(ctx, missis.SearchOptions{Unscoped: true})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("unscoped search", searchUnscoped, ticketE.Ref)

	count, err := client.CountTicketsFiltered(ctx, missis.ListFilter{Unscoped: true})
	if err != nil {
		t.Fatal(err)
	}
	if count != len(unscoped) {
		t.Fatalf("unscoped count = %d, want %d", count, len(unscoped))
	}
	count, err = client.CountTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"p2"}, Groups: []string{"g2"}})
	if err != nil {
		t.Fatal(err)
	}
	if count != len(intersection) {
		t.Fatalf("intersection count = %d, want %d", count, len(intersection))
	}
	if _, err := client.ListTicketsFiltered(ctx, missis.ListFilter{Unscoped: true, Project: "p1"}); err == nil {
		t.Fatal("expected unscoped and legacy project conflict")
	}

	unknown, err := client.ListTicketsFiltered(ctx, missis.ListFilter{Projects: []string{"missing"}})
	if err != nil {
		t.Fatal(err)
	}
	assertRefs("unknown scope", unknown)
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
