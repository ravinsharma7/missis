package model

import (
	"fmt"
	"sort"
)

// OperationDescriptor declares the durable contract for one operation.
// Every declared operation is either state-changing (with projection
// semantics implemented in the projector) or projection-neutral (a marker
// that is accepted, validated, and visible in history/provenance but has no
// projection effect). See specs/missues-issue-specification.v2.md section 19.
type OperationDescriptor struct {
	Name              Operation
	Version           uint32
	ProjectionNeutral bool
	Validate          func(Event) error
}

// declaredOperations is the authoritative list of every operation the system
// may accept. Keep in sync with the Op* constants and the spec's operation
// vocabulary.
var declaredOperations = []Operation{
	OpCreateEntity,
	OpCreatePart,
	OpSetValue,
	OpAddValue,
	OpRetractValue,
	OpRenamePart,
	OpMovePart,
	OpAttachChild,
	OpDetachChild,
	OpRetractSubtree,
	OpRestorePart,
	OpAssertLink,
	OpRetractLink,
	OpAssignOntology,
	OpRemoveOntology,
	OpJoinScope,
	OpLeaveScope,
	OpObserveEffect,
	OpAttachEvidence,
	OpRecordVerification,
	OpSupersedeEvent,
}

var operationRegistry = func() map[Operation]OperationDescriptor {
	registry := make(map[Operation]OperationDescriptor, len(declaredOperations))
	for _, descriptor := range []OperationDescriptor{
		{Name: OpCreateEntity, Version: 1, ProjectionNeutral: true, Validate: func(e Event) error {
			if e.Target.Kind != e.Stream.Kind {
				return fmt.Errorf("create-entity target kind %s must match stream kind %s", e.Target.Kind, e.Stream.Kind)
			}
			return nil
		}},
		{Name: OpCreatePart, Version: 1, Validate: func(e Event) error {
			return requireTargetKind(e, KindPart)
		}},
		{Name: OpSetValue, Version: 1, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			return requireValue(e)
		}},
		{Name: OpAddValue, Version: 1, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			return requireText(e)
		}},
		{Name: OpRetractValue, Version: 1, Validate: func(e Event) error {
			return requireTargetKind(e, KindPart)
		}},
		{Name: OpRenamePart, Version: 1, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			return requireText(e)
		}},
		{Name: OpMovePart, Version: 1, Validate: func(e Event) error {
			// A move-part may relocate to root (nil parent); attach-child is
			// the form that always names a parent.
			return requireTargetKind(e, KindPart)
		}},
		{Name: OpAttachChild, Version: 1, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			return requireValueRef(e)
		}},
		{Name: OpDetachChild, Version: 1, Validate: func(e Event) error {
			return requireTargetKind(e, KindPart)
		}},
		{Name: OpRetractSubtree, Version: 1, Validate: func(e Event) error {
			return requireTargetKind(e, KindPart)
		}},
		{Name: OpRestorePart, Version: 1, Validate: func(e Event) error {
			return requireTargetKind(e, KindPart)
		}},
		{Name: OpAssertLink, Version: 1, Validate: func(e Event) error {
			if e.Value.Ref == nil {
				return fmt.Errorf("assert-link requires a reference value")
			}
			if !ValidRelation(e.Value.Text) {
				return fmt.Errorf("assert-link requires a valid relation, got %q", e.Value.Text)
			}
			return nil
		}},
		{Name: OpRetractLink, Version: 1, Validate: func(e Event) error {
			if e.Value.Ref == nil {
				return fmt.Errorf("retract-link requires a reference value")
			}
			if !ValidRelation(e.Value.Text) {
				return fmt.Errorf("retract-link requires a valid relation, got %q", e.Value.Text)
			}
			return nil
		}},
		{Name: OpAssignOntology, Version: 1, ProjectionNeutral: true, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			return requireValueRef(e)
		}},
		{Name: OpRemoveOntology, Version: 1, ProjectionNeutral: true, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			return requireValueRef(e)
		}},
		{Name: OpJoinScope, Version: 1, Validate: validateScopeTransition},
		{Name: OpLeaveScope, Version: 1, Validate: validateScopeTransition},
		{Name: OpObserveEffect, Version: 1, ProjectionNeutral: true, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			if e.Value.Ref == nil && e.Value.Text == "" {
				return fmt.Errorf("observe-effect requires a reference or text value")
			}
			return nil
		}},
		{Name: OpAttachEvidence, Version: 1, ProjectionNeutral: true, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			if len(e.Sources) == 0 && e.Value.Ref == nil {
				return fmt.Errorf("attach-evidence requires sources or a reference value")
			}
			return nil
		}},
		{Name: OpRecordVerification, Version: 1, ProjectionNeutral: true, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			if e.Value.Ref == nil && e.Value.Text == "" {
				return fmt.Errorf("record-verification requires a reference or text value")
			}
			return nil
		}},
		{Name: OpSupersedeEvent, Version: 1, Validate: func(e Event) error {
			if err := requireTargetKind(e, KindPart); err != nil {
				return err
			}
			if len(e.Supersedes) == 0 {
				return fmt.Errorf("supersede-event requires at least one superseded event")
			}
			return nil
		}},
	} {
		registry[descriptor.Name] = descriptor
	}
	return registry
}()

// LookupOperation returns the descriptor for an operation, if declared.
func LookupOperation(operation Operation) (OperationDescriptor, bool) {
	descriptor, ok := operationRegistry[operation]
	return descriptor, ok
}

// AllOperations returns every declared operation in sorted order.
func AllOperations() []Operation {
	operations := append([]Operation(nil), declaredOperations...)
	sort.Slice(operations, func(i, j int) bool {
		return operations[i] < operations[j]
	})
	return operations
}

func requireTargetKind(e Event, kinds ...Kind) error {
	for _, kind := range kinds {
		if e.Target.Kind == kind {
			return nil
		}
	}
	return fmt.Errorf("operation %s requires target kind %v, got %s", e.Operation, kinds, e.Target.Kind)
}

func requireValue(e Event) error {
	if e.Value.Kind == "" && e.Value.Text == "" && e.Value.Data == nil {
		return fmt.Errorf("operation %s requires a value", e.Operation)
	}
	return nil
}

func requireText(e Event) error {
	if e.Value.Text == "" {
		return fmt.Errorf("operation %s requires a text value", e.Operation)
	}
	return nil
}

func requireValueRef(e Event) error {
	if e.Value.Ref == nil {
		return fmt.Errorf("operation %s requires a reference value", e.Operation)
	}
	return nil
}

func validateScopeTransition(e Event) error {
	if e.Value.Ref == nil {
		return fmt.Errorf("operation %s requires a scope reference", e.Operation)
	}
	if e.Value.Text != RelationMemberOf {
		return fmt.Errorf("operation %s requires relation member-of, got %q", e.Operation, e.Value.Text)
	}
	if e.Target.Kind != KindTicket && e.Target.Kind != KindProject && e.Target.Kind != KindGroup {
		return fmt.Errorf("operation %s target must be a ticket, project, or group", e.Operation)
	}
	if e.Value.Ref.Kind != KindProject && e.Value.Ref.Kind != KindGroup {
		return fmt.Errorf("operation %s scope must be a project or group", e.Operation)
	}
	return nil
}
