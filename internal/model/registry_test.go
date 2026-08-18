package model

import (
	"strings"
	"testing"
	"time"
)

func registryEvent() Event {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	stream := Ref{Kind: KindTicket, Entity: "ticket:test"}
	return Event{
		ID:          EventID("event:registry"),
		Stream:      stream,
		Sequence:    1,
		Operation:   OpSetValue,
		Target:      Ref{Kind: KindPart, Entity: "part:p", Path: []string{"p"}},
		Value:       Value{Kind: ValueKindText, Text: "x"},
		RecordedAt:  now,
		EffectiveAt: now,
		Actor:       ActorRef{Kind: "test", ID: "test"},
	}
}

func TestRegistryCompleteness(t *testing.T) {
	// covers PH1-REG-001
	declared := map[Operation]bool{}
	for _, op := range declaredOperations {
		declared[op] = true
		descriptor, ok := LookupOperation(op)
		if !ok {
			t.Fatalf("declared operation %s has no descriptor", op)
		}
		if descriptor.Name != op || descriptor.Version == 0 {
			t.Fatalf("operation %s has incomplete descriptor: %+v", op, descriptor)
		}
		if descriptor.Validate == nil {
			t.Fatalf("operation %s has no validator", op)
		}
	}
	for op := range operationRegistry {
		if !declared[op] {
			t.Fatalf("registry contains undeclared operation %s", op)
		}
	}
	if len(AllOperations()) != len(declaredOperations) {
		t.Fatalf("AllOperations = %d, want %d", len(AllOperations()), len(declaredOperations))
	}
}

func TestRegistryMarkersAreExplicit(t *testing.T) {
	// covers PH1-REG-002
	wantMarkers := map[Operation]bool{
		OpCreateEntity:       true,
		OpAssignOntology:     true,
		OpRemoveOntology:     true,
		OpJoinScope:          true,
		OpLeaveScope:         true,
		OpObserveEffect:      true,
		OpAttachEvidence:     true,
		OpRecordVerification: true,
	}
	for _, op := range declaredOperations {
		descriptor, _ := LookupOperation(op)
		if descriptor.ProjectionNeutral != wantMarkers[op] {
			t.Fatalf("operation %s ProjectionNeutral = %v, want %v", op, descriptor.ProjectionNeutral, wantMarkers[op])
		}
	}
}

func TestOperationValidation(t *testing.T) {
	// covers PH1-REG-003
	to := Ref{Kind: KindTicket, Entity: "ticket:other"}
	tests := []struct {
		name    string
		mutate  func(*Event)
		wantErr string
	}{
		{name: "create-entity valid", mutate: func(e *Event) {
			e.Operation = OpCreateEntity
			e.Target = Ref{Kind: KindTicket, Entity: "ticket:test"}
		}},
		{name: "create-entity kind mismatch", mutate: func(e *Event) {
			e.Operation = OpCreateEntity
			e.Target = Ref{Kind: KindProject, Entity: "project:x"}
		}, wantErr: "must match stream kind"},
		{name: "set-value valid", mutate: func(e *Event) {}},
		{name: "set-value missing value", mutate: func(e *Event) {
			e.Value = Value{}
		}, wantErr: "requires a value"},
		{name: "move-part valid", mutate: func(e *Event) {
			e.Operation = OpMovePart
			e.Value = Value{Ref: &to}
		}},
		{name: "move-part to root valid", mutate: func(e *Event) {
			e.Operation = OpMovePart
			e.Value = Value{}
		}},
		{name: "attach-child valid", mutate: func(e *Event) {
			e.Operation = OpAttachChild
			e.Value = Value{Ref: &to}
		}},
		{name: "detach-child valid", mutate: func(e *Event) {
			e.Operation = OpDetachChild
			e.Value = Value{}
		}},
		{name: "retract-subtree valid", mutate: func(e *Event) {
			e.Operation = OpRetractSubtree
		}},
		{name: "restore-part valid", mutate: func(e *Event) {
			e.Operation = OpRestorePart
		}},
		{name: "assert-link valid", mutate: func(e *Event) {
			e.Operation = OpAssertLink
			e.Value = Value{Text: "blocked-by", Ref: &to}
		}},
		{name: "assert-link invalid relation", mutate: func(e *Event) {
			e.Operation = OpAssertLink
			e.Value = Value{Text: "not-a-real-relation", Ref: &to}
		}, wantErr: "valid relation"},
		{name: "assign-ontology valid", mutate: func(e *Event) {
			e.Operation = OpAssignOntology
			e.Value = Value{Ref: &to}
		}},
		{name: "assign-ontology missing ref", mutate: func(e *Event) {
			e.Operation = OpAssignOntology
			e.Value = Value{}
		}, wantErr: "requires a reference"},
		{name: "join-scope valid", mutate: func(e *Event) {
			e.Operation = OpJoinScope
			e.Target = Ref{Kind: KindTicket, Entity: "ticket:test"}
			e.Value = Value{Ref: &to}
		}},
		{name: "observe-effect valid text", mutate: func(e *Event) {
			e.Operation = OpObserveEffect
			e.Value = Value{Kind: ValueKindText, Text: "observed"}
		}},
		{name: "observe-effect missing value", mutate: func(e *Event) {
			e.Operation = OpObserveEffect
			e.Value = Value{}
		}, wantErr: "reference or text"},
		{name: "attach-evidence valid sources", mutate: func(e *Event) {
			e.Operation = OpAttachEvidence
			e.Sources = []SourceRef{{Ref: to}}
		}},
		{name: "attach-evidence missing sources", mutate: func(e *Event) {
			e.Operation = OpAttachEvidence
			e.Sources = nil
			e.Value = Value{}
		}, wantErr: "sources or a reference"},
		{name: "record-verification valid", mutate: func(e *Event) {
			e.Operation = OpRecordVerification
			e.Value = Value{Text: "verified", Ref: &to}
		}},
		{name: "supersede-event valid", mutate: func(e *Event) {
			e.Operation = OpSupersedeEvent
			e.Supersedes = []EventID{"event:old"}
		}},
		{name: "supersede-event missing target", mutate: func(e *Event) {
			e.Operation = OpSupersedeEvent
			e.Supersedes = nil
		}, wantErr: "requires at least one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := registryEvent()
			tt.mutate(&event)
			descriptor, ok := LookupOperation(event.Operation)
			if !ok {
				t.Fatalf("operation %s not registered", event.Operation)
			}
			err := descriptor.Validate(event)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLookupOperationUnknown(t *testing.T) {
	// covers PH1-REG-004
	if _, ok := LookupOperation(Operation("unknown-op")); ok {
		t.Fatal("unknown operation must not be registered")
	}
}

func TestMarkerOperationsAreProjectionNeutral(t *testing.T) {
	// covers PH1-REG-005
	base := registryEvent()
	markers := []struct {
		op    Operation
		value Value
	}{
		{op: OpAssignOntology, value: Value{Ref: &Ref{Kind: KindPart, Entity: "part:onto"}}},
		{op: OpJoinScope, value: Value{Ref: &Ref{Kind: KindProject, Entity: "project:s"}}},
		{op: OpObserveEffect, value: Value{Text: "effect"}},
	}
	for _, marker := range markers {
		event := base
		event.Operation = marker.op
		event.Value = marker.value
		ticketID := TicketID(event.Stream.Entity)
		proj, err := ProjectTicket([]Event{event}, ticketID, event.EffectiveAt, event.RecordedAt)
		if err != nil {
			t.Fatalf("%s: %v", marker.op, err)
		}
		if proj.TicketID != ticketID {
			t.Fatalf("%s must not change the projection ticket", marker.op)
		}
		if len(proj.Parts) != 0 {
			t.Fatalf("%s must not create parts: %+v", marker.op, proj.Parts)
		}
	}
}
