package application

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestScopeEntitySchemaPartsAndShow(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	if _, err := svc.NewEntity(ctx, missis.RequestContext{}, missis.EntityOptions{Kind: "project", ID: "proj", Title: "Project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, missis.RequestContext{}, missis.SetValue{Target: "project:proj/schema/status", Value: "status", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	proj, err := svc.ShowEntity(ctx, "project:proj", missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part, ok := proj.Parts["schema/status"]
	if !ok {
		t.Fatalf("schema/status missing from project show: %+v", proj.Parts)
	}
	if part.ValueKind != string(model.ValueKindText) || part.Value != "status" {
		t.Fatalf("schema/status part = %+v", part)
	}
}

func TestPluginKindIsValidatedBeforeAppend(t *testing.T) {
	svc := openFixed(t, fixedClock{fixedNow()})
	if err := svc.RegisterPluginKind(plugin.KindRegistration{
		Manifest: plugin.Manifest{ID: "plugin.card", Version: "1"},
		Kind:     model.ValueKind("plugin/card"),
		Schema:   "plugin/card/v1",
		Validate: func(value model.Value) error {
			if value.Text != "ok" {
				return fmt.Errorf("plugin card must be ok")
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(context.Background(), missis.RequestContext{}, missis.NewTicketOptions{Title: "Plugin value"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(context.Background(), missis.RequestContext{}, missis.SetValue{
		Target: created.Ref + "/card", Value: "rejected", Kind: model.ValueKind("plugin/card"),
	}); err == nil {
		t.Fatal("plugin validator rejection did not stop append")
	}
	if _, err := svc.Set(context.Background(), missis.RequestContext{}, missis.SetValue{
		Target: created.Ref + "/card", Value: "ok", Kind: model.ValueKind("plugin/card"),
	}); err != nil {
		t.Fatalf("valid plugin value rejected: %v", err)
	}
	projection, err := svc.ShowTicket(context.Background(), created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if projection.Parts["card"].ValueKind != "plugin/card" {
		t.Fatalf("stored plugin kind = %q", projection.Parts["card"].ValueKind)
	}
	if _, err := svc.Set(context.Background(), missis.RequestContext{}, missis.SetValueData{
		Target: created.Ref + "/code", Kind: model.ValueKindCodeRef,
		Data: model.CodeRef{Repository: "github.com/example/project", Commit: "abc123", Path: "main.go"},
	}); err != nil {
		t.Fatalf("structured CodeRef rejected: %v", err)
	}
	projection, err = svc.ShowTicket(context.Background(), created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	code, ok := projection.Parts["code"].Value.(model.CodeRef)
	if !ok || code.Commit != "abc123" || code.Path != "main.go" {
		t.Fatalf("stored CodeRef = %#v", projection.Parts["code"].Value)
	}
}

func TestMalformedDeclarationRejectedAtWrite(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	if _, err := svc.NewEntity(ctx, missis.RequestContext{}, missis.EntityOptions{Kind: "project", ID: "proj", Title: "Project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, missis.RequestContext{}, missis.SetValue{Target: "project:proj/schema/evidence/*", Value: "evidence", Kind: model.ValueKindText}); err == nil {
		t.Fatal("wildcard declaration path must be rejected")
	}
	if _, err := svc.Set(ctx, missis.RequestContext{}, missis.SetValue{Target: "project:proj/schema/status", Value: "bogus", Kind: model.ValueKindText}); err == nil {
		t.Fatal("unknown declaration kind must be rejected")
	}
}

func TestDeclarationEnforcementOnTicketWrites(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Enforced"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "has-home", Target: "project:safedesign", Add: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: "project:safedesign/schema/deps", Value: "list[ref]", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: created.Ref + "/deps", Value: "scalar", Kind: model.ValueKindScalar}); err == nil {
		t.Fatal("scalar write against list[ref] declaration must be rejected")
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: created.Ref + "/deps", Value: "#1", Kind: model.ValueKindList}); err == nil {
		t.Fatal("whole-list SetValue against declared list[ref] must be rejected in favor of --add")
	}
	if _, err := svc.Set(ctx, req, missis.AddValue{Target: created.Ref + "/deps", Value: "#1"}); err != nil {
		t.Fatalf("ref element add must be accepted: %v", err)
	}
	if _, err := svc.Set(ctx, req, missis.AddValue{Target: created.Ref + "/deps", Value: "not-a-ref"}); err == nil {
		t.Fatal("non-ref element add must be rejected")
	}
	if _, err := svc.Set(ctx, req, missis.AddValue{Target: created.Ref + "/deps", Value: created.Ref}); err != nil {
		t.Fatalf("second ref element add must be accepted: %v", err)
	}
}

func TestUndeclaredKeyRequiresExplicitKind(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "NoFallback"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, missis.RequestContext{}, missis.SetValue{Target: created.Ref + "/notes", Value: "scratch"}); err == nil {
		t.Fatal("write without explicit kind must be rejected (no inference)")
	}
}

func TestDeclaredKindWinsAndShowsInProjection(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Rendered"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "has-home", Target: "project:safedesign", Add: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: "project:safedesign/schema/problem", Value: "markdown", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: created.Ref + "/problem", Value: "body"}); err != nil {
		t.Fatal(err)
	}
	proj, err := svc.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	part := proj.Parts["problem"]
	if part.ValueKind != string(model.ValueKindMarkdown) {
		t.Fatalf("declared kind must win over writer kind: %s", part.ValueKind)
	}
	if !strings.Contains(part.DeclaredSchema, "schema/problem") || !strings.Contains(part.DeclaredSchema, "safedesign") {
		t.Fatalf("declared schema metadata missing: %q", part.DeclaredSchema)
	}
}

func TestLinkLegalityEnforced(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Linked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "has-home", Target: "project:safedesign", Add: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: "project:safedesign/schema/links/supports", Value: "ref[ticket|part]", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "supports", Target: "artifact:xyz", Add: true}); err == nil {
		t.Fatal("artifact target must be rejected by supports legality declaration")
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "supports", Target: "#1", Add: true}); err != nil {
		t.Fatalf("ticket target must be allowed: %v", err)
	}
}

func TestReimportRejectsAtomically(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{}
	if _, err := svc.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Import"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLink(ctx, req, missis.LinkOptions{Ref: created.Ref + "/links", Relation: "has-home", Target: "project:safedesign", Add: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, req, missis.SetValue{Target: "project:safedesign/schema/problem", Value: "scalar", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}
	before, err := svc.ShowHistory(ctx, created.Ref, missis.HistoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content := "## title\n\n## problem\n\nmarkdown body violates scalar declaration\n"
	if _, err := svc.ReimportMarkdown(ctx, req, missis.ImportOptions{Ref: created.Ref, Content: content, Artifact: "artifact:test.md"}); err == nil {
		t.Fatal("reimport violating declarations must be rejected")
	}
	after, err := svc.ShowHistory(ctx, created.Ref, missis.HistoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("rejected import must write nothing: history %d -> %d", len(before), len(after))
	}
}
