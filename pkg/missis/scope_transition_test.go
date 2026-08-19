package missis_test

import (
	"context"
	"testing"

	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestJoinScopeAndLeaveScopeSDK(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	if _, err := client.NewEntity(ctx, req(), missis.EntityOptions{Kind: "group", ID: "eng", Title: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	created, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.JoinScope(ctx, req(), missis.ScopeOptions{Entity: created.Ref, Scope: "group:eng"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Operation != "join-scope" {
		t.Fatalf("operation = %q", res.Operation)
	}
	refs, err := client.ShowReferences(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, link := range refs {
		if link.Relation == "member-of" && link.Direction == "asserted" && len(link.Assertions) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected member-of assertion: %+v", refs)
	}
	if _, err := client.LeaveScope(ctx, req(), missis.ScopeOptions{Entity: created.Ref, Scope: "group:eng"}); err != nil {
		t.Fatal(err)
	}
}
