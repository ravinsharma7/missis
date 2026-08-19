package missis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestMoveHomeConvenience(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	for _, p := range []string{"a", "b"} {
		if _, err := client.NewEntity(ctx, req(), missis.EntityOptions{Kind: "project", ID: p, Title: p}); err != nil {
			t.Fatal(err)
		}
	}
	created, err := client.NewTicket(ctx, req(), missis.NewTicketOptions{Title: "Homed", Project: "a"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.MoveHome(ctx, req(), created.Ref, "a", "b", "reorg")
	if err != nil {
		t.Fatal(err)
	}
	if res.Warning != "" {
		t.Fatalf("move must not warn: %q", res.Warning)
	}
	if res.Operation != "move-link" {
		t.Fatalf("operation = %q", res.Operation)
	}
	refs, err := client.ShowReferences(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	homeCount := 0
	for _, link := range refs {
		if link.Relation == "has-home" && link.Direction == "asserted" {
			homeCount++
			if !strings.Contains(link.To, "project:b") {
				t.Fatalf("home should point to b: %+v", link)
			}
		}
	}
	if homeCount != 1 {
		t.Fatalf("has-home assertions = %d", homeCount)
	}
}
