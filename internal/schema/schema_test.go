package schema

import (
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

var at = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

func decl(scope Scope, scopeID string, prefix []string, kind string) Declaration {
	spec, err := ParseKind(kind)
	if err != nil {
		panic(err)
	}
	return Declaration{
		Scope:       scope,
		ScopeID:     scopeID,
		Prefix:      prefix,
		Kind:        spec,
		EventRef:    "event:test",
		EffectiveAt: at.Add(-time.Hour),
		KnownAt:     at.Add(-time.Hour),
	}
}

func TestResolveDeclaredKindRegardlessOfContent(t *testing.T) {
	r := NewResolver([]Declaration{decl(ScopeProject, "safedesign", []string{"status"}, "status")})
	got := r.Resolve(TicketContext{ProjectID: "safedesign"}, []string{"status"}, model.ValueKindMarkdown, at)
	if got.Declared == nil || got.Declared.String() != "status" {
		t.Fatalf("declared kind = %v, want status", got.Declared)
	}
}

func TestSubtreeInheritanceAndOverride(t *testing.T) {
	r := NewResolver([]Declaration{
		decl(ScopeProject, "safedesign", []string{"evidence"}, "evidence"),
		decl(ScopeProject, "safedesign", []string{"evidence", "run-417"}, "code-ref"),
	})
	ticket := TicketContext{ProjectID: "safedesign"}
	for key, want := range map[string]string{
		"evidence":             "evidence",
		"evidence/race-test":   "evidence",
		"evidence/run-417":     "code-ref",
		"evidence/run-417/log": "code-ref",
	} {
		got := r.Resolve(ticket, strings.Split(key, "/"), model.ValueKindText, at)
		if got.Declared == nil || got.Declared.String() != want {
			t.Fatalf("%s = %v, want %s", key, got.Declared, want)
		}
	}
}

func TestUndeclaredResolvesStoredKindOnly(t *testing.T) {
	r := NewResolver(nil)
	ticket := TicketContext{}
	if got := r.Resolve(ticket, []string{"notes"}, model.ValueKindMarkdown, at); got.Declared != nil || got.Stored != model.ValueKindMarkdown {
		t.Fatalf("stored kind = %v, want markdown", got.Stored)
	}
	if got := r.Resolve(ticket, []string{"notes"}, "", at); got.Declared != nil || got.Stored != "" {
		t.Fatalf("missing kind must stay empty (no implicit text), got %q", got.Stored)
	}
}

func TestRejectionReasonFields(t *testing.T) {
	r := NewResolver([]Declaration{decl(ScopeProject, "safedesign", []string{"deps"}, "list[ref]")})
	ticket := TicketContext{ProjectID: "safedesign"}
	rej := r.ValidateWrite(ticket, []string{"deps"}, ValueShape{Kind: model.ValueKindScalar}, at)
	if rej == nil {
		t.Fatal("expected rejection")
	}
	if rej.Scope != "project:safedesign" || rej.Pattern != "schema/deps" {
		t.Fatalf("scope/pattern = %q / %q", rej.Scope, rej.Pattern)
	}
	if rej.Expected != "list[ref]" || rej.Proposed != "scalar" {
		t.Fatalf("expected/proposed = %q / %q", rej.Expected, rej.Proposed)
	}
	if rej.VersionHash == "" {
		t.Fatal("version hash must be present")
	}
	if !strings.Contains(rej.Reason, "schema/deps") || !strings.Contains(rej.Reason, "list[ref]") {
		t.Fatalf("reason lacks context: %s", rej.Reason)
	}
}

func TestAcceptedWriteStoresDeclaredKind(t *testing.T) {
	for text, want := range map[string]model.ValueKind{
		"list[ref]":        model.ValueKindList,
		"map[text:scalar]": model.ValueKindMap,
		"ref[ticket|part]": model.ValueKindRef,
		"evidence":         model.ValueKindEvidence,
	} {
		spec, err := ParseKind(text)
		if err != nil {
			t.Fatal(err)
		}
		if spec.StoredKind() != want {
			t.Fatalf("%s stores %s, want %s", text, spec.StoredKind(), want)
		}
	}
}

func TestLinkLegalityRejectsIllegalTarget(t *testing.T) {
	r := NewResolver([]Declaration{decl(ScopeProject, "safedesign", []string{"links", "supports"}, "ref[ticket|part]")})
	ticket := TicketContext{ProjectID: "safedesign"}
	if rej := r.ValidateLink(ticket, "supports", model.KindArtifact, at); rej == nil {
		t.Fatal("artifact target must be rejected")
	}
	if rej := r.ValidateLink(ticket, "supports", model.KindPart, at); rej != nil {
		t.Fatalf("part target must be allowed, got %s", rej.Reason)
	}
	if rej := r.ValidateLink(ticket, "blocks", model.KindArtifact, at); rej != nil {
		t.Fatalf("undeclared relation must be open-world, got %s", rej.Reason)
	}
}

func TestDeterminismAcrossSessions(t *testing.T) {
	decls := []Declaration{
		decl(ScopeProject, "safedesign", []string{"evidence"}, "evidence"),
		decl(ScopeGroup, "eng", []string{"evidence", "run-417"}, "code-ref"),
		decl(ScopeProject, "safedesign", []string{"deps"}, "list[ref]"),
		decl(ScopeGroup, "qa", []string{"status"}, "scalar"),
	}
	ticket := TicketContext{ProjectID: "safedesign", Groups: []string{"eng", "qa"}}
	shuffled := []Declaration{decls[3], decls[0], decls[2], decls[1]}
	r1, r2 := NewResolver(decls), NewResolver(shuffled)
	keys := [][]string{{"evidence"}, {"evidence", "run-417"}, {"deps"}, {"status"}}
	for _, key := range keys {
		a, b := r1.Resolve(ticket, key, model.ValueKindText, at), r2.Resolve(ticket, key, model.ValueKindText, at)
		if a.Declared.String() != b.Declared.String() {
			t.Fatalf("%v resolved %s vs %s", key, a.Declared, b.Declared)
		}
	}
	if h1, h2 := r1.VersionHash(ticket, at), r2.VersionHash(ticket, at); h1 != h2 {
		t.Fatalf("version hashes differ across sessions: %s vs %s", h1, h2)
	}
}

func TestNoImplicitKindWithoutDeclaration(t *testing.T) {
	r := NewResolver(nil)
	got := r.Resolve(TicketContext{}, []string{"status"}, "", at)
	if got.Declared != nil || got.Stored != "" {
		t.Fatalf("no declaration must imply no kind; got declared=%v stored=%q", got.Declared, got.Stored)
	}
}

func TestBitemporalSelectionAndNoRetroactiveInvalidation(t *testing.T) {
	early := decl(ScopeProject, "safedesign", []string{"problem"}, "text")
	early.EventRef = "event:early"
	late := decl(ScopeProject, "safedesign", []string{"problem"}, "markdown")
	late.EventRef = "event:late"
	late.EffectiveAt = at.Add(time.Hour)
	late.KnownAt = at.Add(time.Hour)
	r := NewResolver([]Declaration{early, late})
	ticket := TicketContext{ProjectID: "safedesign"}
	if got := r.Resolve(ticket, []string{"problem"}, "", at); got.Declared.String() != "text" {
		t.Fatalf("before late declaration = %s, want text", got.Declared)
	}
	if got := r.Resolve(ticket, []string{"problem"}, "", at.Add(2*time.Hour)); got.Declared.String() != "markdown" {
		t.Fatalf("after late declaration = %s, want markdown", got.Declared)
	}
	if rej := r.ValidateWrite(ticket, []string{"problem"}, ValueShape{Kind: model.ValueKindText}, at); rej != nil {
		t.Fatalf("write under earlier schema must stay valid, got %s", rej.Reason)
	}
	if rej := r.ValidateWrite(ticket, []string{"problem"}, ValueShape{Kind: model.ValueKindText}, at.Add(2*time.Hour)); rej == nil {
		t.Fatal("write under later schema must be rejected")
	}
}

func TestMalformedDeclarationsRejected(t *testing.T) {
	badKinds := []string{"", "wat", "list[]", "list[list[x]]", "map[a]", "map[a:b:c]", "ref[]", "ref[wat]"}
	for _, text := range badKinds {
		if _, err := ParseKind(text); err == nil {
			t.Fatalf("ParseKind(%q) must fail", text)
		}
	}
	badPaths := [][]string{nil, {"type"}, {"type", "bug"}, {"evidence", "*"}, {"Bad Segment"}}
	for _, path := range badPaths {
		if _, _, err := ParseDeclarationPath(path); err == nil {
			t.Fatalf("ParseDeclarationPath(%v) must fail", path)
		}
	}
}

func TestTypeQualifiedWinsAtEqualPrefix(t *testing.T) {
	typed := decl(ScopeProject, "safedesign", []string{"evidence"}, "code-ref")
	typed.TypeQualified = "bug"
	plain := decl(ScopeProject, "safedesign", []string{"evidence"}, "evidence")
	r := NewResolver([]Declaration{plain, typed})
	bug := TicketContext{ProjectID: "safedesign", Types: []string{"bug"}}
	other := TicketContext{ProjectID: "safedesign", Types: []string{"experiment"}}
	if got := r.Resolve(bug, []string{"evidence"}, "", at); got.Declared.String() != "code-ref" {
		t.Fatalf("type-qualified = %s, want code-ref", got.Declared)
	}
	if got := r.Resolve(other, []string{"evidence"}, "", at); got.Declared.String() != "evidence" {
		t.Fatalf("unqualified fallback = %s, want evidence", got.Declared)
	}
}

func TestScopeTieBreakNearestScopeWins(t *testing.T) {
	r := NewResolver([]Declaration{
		decl(ScopeProject, "safedesign", []string{"status"}, "scalar"),
		decl(ScopeGroup, "eng", []string{"status"}, "status"),
	})
	ticket := TicketContext{ProjectID: "safedesign", Groups: []string{"eng"}}
	if got := r.Resolve(ticket, []string{"status"}, "", at); got.Declared.String() != "scalar" {
		t.Fatalf("project must beat group at equal prefix, got %s", got.Declared)
	}
}

func TestRendererDistinguishesCompositesAndKinds(t *testing.T) {
	r := NewResolver([]Declaration{
		decl(ScopeProject, "safedesign", []string{"deps"}, "list[ref]"),
		decl(ScopeProject, "safedesign", []string{"tags"}, "list[text]"),
		decl(ScopeProject, "safedesign", []string{"evidence"}, "evidence"),
		decl(ScopeProject, "safedesign", []string{"problem"}, "markdown"),
	})
	ticket := TicketContext{ProjectID: "safedesign"}
	deps := r.Resolve(ticket, []string{"deps"}, "", at)
	tags := r.Resolve(ticket, []string{"tags"}, "", at)
	if deps.Declared.String() == tags.Declared.String() {
		t.Fatalf("list[ref] and list[text] must be distinguishable, both %s", deps.Declared)
	}
	ev := r.Resolve(ticket, []string{"evidence"}, "", at)
	problem := r.Resolve(ticket, []string{"problem"}, "", at)
	if ev.Declared.String() == problem.Declared.String() {
		t.Fatalf("evidence and markdown must be distinguishable, both %s", ev.Declared)
	}
}

func TestParseDeclarationPathReservesType(t *testing.T) {
	prefix, qualified, err := ParseDeclarationPath([]string{"type", "bug", "evidence", "run-417"})
	if err != nil {
		t.Fatal(err)
	}
	if qualified != "bug" || strings.Join(prefix, "/") != "evidence/run-417" {
		t.Fatalf("got qualified=%s prefix=%v", qualified, prefix)
	}
	if _, _, err := ParseDeclarationPath([]string{"type"}); err == nil {
		t.Fatal("bare type must fail")
	}
}
