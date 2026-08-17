package blackbox

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/implementation/model"
	"github.com/ravinsharma7/missis/implementation/store"
)

func TestCLIFlagsMapToOperations(t *testing.T) {
	t.Parallel()
	// covers PH1-REG-003
	storePath := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, storePath, "Ops")
	ref := created["ref"].(string)

	if result := runMissis(t, storePath, "set", "--json", ref+"/notes", "hello"); result.code != 0 {
		t.Fatalf("set-value: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, storePath, "set", "--json", ref+"/tag", "--add", "x"); result.code != 0 {
		t.Fatalf("add-value: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, storePath, "set", "--json", ref+"/notes", "--retract", "--reason", "gone"); result.code != 0 {
		t.Fatalf("retract-value: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, storePath, "set", "--json", ref+"/notes", "--name", "memo"); result.code != 0 {
		t.Fatalf("rename-part: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, storePath, "set", "--json", ref+"/section", "s"); result.code != 0 {
		t.Fatalf("create section: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, storePath, "set", "--json", ref+"/memo", "--parent", ref+"/section"); result.code != 0 {
		t.Fatalf("move-part: %d %s", result.code, result.stderr)
	}
	second := newTicket(t, storePath, "OpsTarget")
	if result := runMissis(t, storePath, "set", "--json", ref+"/links", "--add", "blocked-by:"+second["ref"].(string)); result.code != 0 {
		t.Fatalf("assert-link: %d %s", result.code, result.stderr)
	}
	if result := runMissis(t, storePath, "set", "--json", ref+"/links", "--retract", "blocked-by:"+second["ref"].(string), "--reason", "done"); result.code != 0 {
		t.Fatalf("retract-link: %d %s", result.code, result.stderr)
	}
	first := mustJSON(t, runMissis(t, storePath, "set", "--json", ref+"/status", "doing"))
	alias := first["event"].(string)
	if result := runMissis(t, storePath, "set", "--json", ref+"/status", "blocked", "--reason", "waiting", "--supersedes", alias); result.code != 0 {
		t.Fatalf("supersede-event: %d %s", result.code, result.stderr)
	}

	history := mustJSON(t, runMissis(t, storePath, "show", ref, "--history", "--json"))
	seen := map[string]bool{}
	for _, raw := range history["events"].([]any) {
		event := raw.(map[string]any)
		seen[event["operation"].(string)] = true
	}
	for _, want := range []string{
		"set-value", "add-value", "retract-value", "rename-part",
		"move-part", "assert-link", "retract-link", "supersede-event",
	} {
		if !seen[want] {
			t.Fatalf("history missing operation %s; got %v", want, seen)
		}
	}
}

func TestMarkerVisibleInHistory(t *testing.T) {
	t.Parallel()
	// covers PH1-REG-005
	storePath := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, storePath, "Marker")
	ref := created["ref"].(string)
	ticketID := created["id"].(string)

	s, err := store.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.LoadTicketEvents(model.TicketID(ticketID))
	if err != nil {
		t.Fatal(err)
	}
	var partID model.PartID
	for _, event := range events {
		if event.Operation == model.OpCreatePart {
			partID = model.PartID(event.Target.Entity)
			break
		}
	}
	if partID == "" {
		t.Fatal("no part found to attach the marker to")
	}
	now := time.Now().UTC()
	marker := model.Event{
		ID:          model.EventID("event:marker"),
		Stream:      model.Ref{Kind: model.KindTicket, Entity: ticketID},
		Operation:   model.OpAssignOntology,
		Target:      model.Ref{Kind: model.KindPart, Entity: string(partID), Path: []string{"title"}},
		Value:       model.Value{Ref: &model.Ref{Kind: model.KindPart, Entity: "part:onto"}},
		RecordedAt:  now,
		EffectiveAt: now,
		Actor:       model.ActorRef{Kind: "test", ID: "test"},
	}
	if _, err := s.AppendBatch([]model.Event{marker}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	history := mustJSON(t, runMissis(t, storePath, "show", ref, "--history", "--json"))
	found := false
	for _, raw := range history["events"].([]any) {
		event := raw.(map[string]any)
		if event["operation"] == "assign-ontology" {
			found = true
		}
	}
	if !found {
		t.Fatalf("marker operation must be visible in history: %v", history)
	}
}
