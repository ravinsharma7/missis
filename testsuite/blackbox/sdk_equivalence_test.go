package blackbox

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestCLIWritesSDKReads(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "cli.db")
	first := newTicket(t, store, "CLI side")
	second := newTicket(t, store, "Target")
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/status", "doing", "--kind", "status"); result.code != 0 {
		t.Fatalf("set status: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/problem", "shared body", "--kind", "text"); result.code != 0 {
		t.Fatalf("set problem: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", first["ref"].(string)+"/links", "--add", "blocked-by:"+second["ref"].(string)); result.code != 0 {
		t.Fatalf("link: %d %s", result.code, result.stderr)
	}

	svc, err := application.OpenPath(store)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	ref := first["ref"].(string)

	proj, err := client.ShowTicket(ctx, ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Title != "CLI side" || proj.Status != "doing" {
		t.Fatalf("projection = %+v", proj)
	}
	if got := proj.Parts["problem"].Value; got != "shared body" {
		t.Fatalf("problem value = %v", got)
	}

	refs, err := client.ShowReferences(ctx, ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Relation != "blocked-by" {
		t.Fatalf("references = %+v", refs)
	}

	search, err := client.Search(ctx, missis.SearchOptions{Query: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 1 {
		t.Fatalf("search = %+v", search)
	}
}

func TestSDKWritesCLIReads(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "sdk.db")
	svc, err := application.OpenPath(store)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	ctx := context.Background()
	first, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "SDK side"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValue{Target: first.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValue{Target: first.Ref + "/problem", Value: "shared body", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetLink(ctx, missis.RequestContext{Actor: "test"}, missis.LinkOptions{Ref: first.Ref + "/links", Relation: "blocked-by", Target: second.Ref, Add: true}); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	shown := mustJSON(t, runMissis(t, store, "show", "--json", first.Ref))
	if shown["status"] != "doing" || shown["title"] != "SDK side" {
		t.Fatalf("show = %v", shown)
	}
	parts := shown["parts"].(map[string]any)
	if parts["problem"].(map[string]any)["value"] != "shared body" {
		t.Fatalf("parts = %v", parts)
	}
	refs := mustJSON(t, runMissis(t, store, "show", "--json", first.Ref, "--references"))
	if len(refs["links"].([]any)) != 1 {
		t.Fatalf("references = %v", refs)
	}
}
