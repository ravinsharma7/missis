package blackbox

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
	"github.com/ravinsharma7/missis/pkg/missis/render"
)

func parseJSONBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, raw)
	}
	return body
}

func TestRenderParityTextAndJSON(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "parity.db")
	first := newTicket(t, store, "Parity")
	second := newTicket(t, store, "Target")
	ref := first["ref"].(string)
	if result := runMissis(t, store, "set", "--json", ref+"/status", "doing", "--kind", "status"); result.code != 0 {
		t.Fatalf("set status: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/problem", "body text", "--kind", "text"); result.code != 0 {
		t.Fatalf("set problem: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/type", "--add", "bug"); result.code != 0 {
		t.Fatalf("add type: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, store, "set", "--json", ref+"/links", "--add", "blocked-by:"+second["ref"].(string)); result.code != 0 {
		t.Fatalf("link: %d %s", result.code, result.stderr)
	}

	svc, err := application.OpenPath(store)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	proj, err := client.ShowTicket(ctx, ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gotText, err := render.ShowTicket(proj, "text")
	if err != nil {
		t.Fatal(err)
	}
	wantText := runMissis(t, store, "show", ref).stdout
	if gotText != wantText {
		t.Fatalf("text parity mismatch:\nrender:\n%s\ncli:\n%s", gotText, wantText)
	}

	gotJSON, err := render.ShowTicket(proj, "json")
	if err != nil {
		t.Fatal(err)
	}
	gotBody := parseJSONBody(t, gotJSON)
	wantBody := parseJSONBody(t, runMissis(t, store, "show", "--json", ref).stdout)
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("json parity mismatch:\nrender: %v\ncli:     %v", gotBody, wantBody)
	}

	summaries, err := client.ListTicketSummaries(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	gotList, err := render.ShowList(summaries, "text")
	if err != nil {
		t.Fatal(err)
	}
	if gotList != runMissis(t, store, "show").stdout {
		t.Fatalf("list text parity mismatch:\nrender:\n%s", gotList)
	}
	gotListJSON, err := render.ShowList(summaries, "json")
	if err != nil {
		t.Fatal(err)
	}
	gotListBody := parseJSONBody(t, gotListJSON)
	wantListBody := parseJSONBody(t, runMissis(t, store, "show", "--json").stdout)
	if !reflect.DeepEqual(gotListBody, wantListBody) {
		t.Fatalf("list json parity mismatch:\nrender: %v\ncli:     %v", gotListBody, wantListBody)
	}

	events, err := client.ShowHistory(ctx, ref, missis.HistoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gotHistory, err := render.ShowHistory(events, "json")
	if err != nil {
		t.Fatal(err)
	}
	gotHistoryBody := parseJSONBody(t, gotHistory)
	wantHistoryBody := parseJSONBody(t, runMissis(t, store, "show", ref, "--history", "--json").stdout)
	if !reflect.DeepEqual(gotHistoryBody, wantHistoryBody) {
		t.Fatalf("history json parity mismatch:\nrender: %v\ncli:     %v", gotHistoryBody, wantHistoryBody)
	}

	links, err := client.ShowReferences(ctx, ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gotRefs, err := render.ShowReferences(links, "json")
	if err != nil {
		t.Fatal(err)
	}
	gotRefsBody := parseJSONBody(t, gotRefs)
	wantRefsBody := parseJSONBody(t, runMissis(t, store, "show", ref, "--references", "--json").stdout)
	if !reflect.DeepEqual(gotRefsBody, wantRefsBody) {
		t.Fatalf("references json parity mismatch:\nrender: %v\ncli:     %v", gotRefsBody, wantRefsBody)
	}

	start, err := client.ResolveAnyRef(ctx, ref, now)
	if err != nil {
		t.Fatal(err)
	}
	edges, err := client.ShowLineage(ctx, ref, missis.LineageOptions{Direction: "both", Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	gotLineage, err := render.ShowLineage(edges, start, "json")
	if err != nil {
		t.Fatal(err)
	}
	gotLineageBody := parseJSONBody(t, gotLineage)
	wantLineageBody := parseJSONBody(t, runMissis(t, store, "show", ref, "--lineage", "--json").stdout)
	if !reflect.DeepEqual(gotLineageBody, wantLineageBody) {
		t.Fatalf("lineage json parity mismatch:\nrender: %v\ncli:     %v", gotLineageBody, wantLineageBody)
	}

	gotMarkdown := render.ShowMarkdown(proj, links)
	wantMarkdown := runMissis(t, store, "show", ref, "--format", "markdown").stdout
	if gotMarkdown != wantMarkdown {
		t.Fatalf("markdown parity mismatch:\nrender:\n%s\ncli:\n%s", gotMarkdown, wantMarkdown)
	}
}

func TestRenderParityEmptyParts(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "parity-empty.db")
	created := newTicket(t, store, "Plain")
	ref := created["ref"].(string)

	svc, err := application.OpenPath(store)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()

	proj, err := client.ShowTicket(ctx, ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gotText, err := render.ShowTicket(proj, "text")
	if err != nil {
		t.Fatal(err)
	}
	if gotText != runMissis(t, store, "show", ref).stdout {
		t.Fatalf("empty-parts text parity mismatch:\nrender:\n%s", gotText)
	}
	gotJSON, err := render.ShowTicket(proj, "json")
	if err != nil {
		t.Fatal(err)
	}
	gotBody := parseJSONBody(t, gotJSON)
	wantBody := parseJSONBody(t, runMissis(t, store, "show", "--json", ref).stdout)
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("empty-parts json parity mismatch:\nrender: %v\ncli:     %v", gotBody, wantBody)
	}
}
