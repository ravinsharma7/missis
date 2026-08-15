package model

import "time"

// This file is the bootstrap data-model draft for Phase 1. The types are
// intended to become the concrete implementation contract, while the
// projection and validation functions are signatures to be filled in next.
//
// The public command spelling is:
//
//	missis new ...
//	missis show ...
//	missis set ...
//
// The current canonical specification is:
//
//	specs/missues-issue-specification.v2.md

type ID string

type (
	EventID  = ID
	PartID   = ID
	LinkID   = ID
	TicketID = ID
	RunID    = ID
	BatchID  = ID
)

type Kind string

const (
	KindTicket   Kind = "ticket"
	KindPart     Kind = "part"
	KindEvent    Kind = "event"
	KindProject  Kind = "project"
	KindGroup    Kind = "group"
	KindRun      Kind = "run"
	KindCode     Kind = "code"
	KindGit      Kind = "git"
	KindArtifact Kind = "artifact"
)

// Ref is a canonical reference. Entity is the immutable identity. Path is an
// optional human-readable alias and is never canonical identity.
type Ref struct {
	Kind   Kind
	Entity string
	Path   []string
}

type ActorRef struct {
	Kind string
	ID   string
	Name string
}

type Span struct {
	StartLine *int
	EndLine   *int
	StartByte *int
	EndByte   *int
}

type SourceRef struct {
	Ref       Ref
	MediaType string
	Span      *Span
}

type ValueKind string

const (
	ValueKindText       ValueKind = "text"
	ValueKindMarkdown   ValueKind = "markdown"
	ValueKindScalar     ValueKind = "scalar"
	ValueKindStatus     ValueKind = "status"
	ValueKindPriority   ValueKind = "priority"
	ValueKindMap        ValueKind = "map"
	ValueKindList       ValueKind = "list"
	ValueKindRef        ValueKind = "ref"
	ValueKindCodeRef    ValueKind = "code-ref"
	ValueKindGitRef     ValueKind = "git-ref"
	ValueKindEvidence   ValueKind = "evidence"
	ValueKindVerification ValueKind = "verification"
	ValueKindJSON       ValueKind = "json"
	ValueKindArtifact   ValueKind = "artifact"
	ValueKindAnnotation ValueKind = "annotation"
)

// Value is the payload stored on a part. Retracted is a projection flag, not
// the mechanism for removing history; retraction is represented by events.
type Value struct {
	Kind      ValueKind
	Text      string
	Data      any
	List      []string
	Ref       *Ref
	Retracted bool
}

type Operation string

const (
	OpCreateEntity     Operation = "create-entity"
	OpCreatePart       Operation = "create-part"
	OpSetValue         Operation = "set-value"
	OpAddValue         Operation = "add-value"
	OpRetractValue     Operation = "retract-value"
	OpRenamePart       Operation = "rename-part"
	OpMovePart         Operation = "move-part"
	OpAttachChild      Operation = "attach-child"
	OpDetachChild      Operation = "detach-child"
	OpRetractSubtree   Operation = "retract-subtree"
	OpRestorePart      Operation = "restore-part"
	OpAssertLink       Operation = "assert-link"
	OpRetractLink      Operation = "retract-link"
	OpAssignOntology   Operation = "assign-ontology"
	OpRemoveOntology   Operation = "remove-ontology"
	OpJoinScope        Operation = "join-scope"
	OpLeaveScope       Operation = "leave-scope"
	OpObserveEffect    Operation = "observe-effect"
	OpAttachEvidence   Operation = "attach-evidence"
	OpRecordVerification Operation = "record-verification"
	OpSupersedeEvent   Operation = "supersede-event"
)

type OntologyRef struct {
	ID      string
	Version string
	Hash    string
}

type InvocationRef struct {
	ID string
}

type Effect struct {
	Kind string
	Ref  *Ref

	Before *Value
	After  *Value

	ObservedAt *time.Time
	Evidence   []Ref
}

// Event is the authoritative append-only record.
type Event struct {
	ID       EventID
	AliasSeq uint64
	Stream   Ref
	Sequence uint64
	BatchID  *BatchID

	Operation Operation
	Target    Ref
	Value     Value

	RecordedAt  time.Time
	EffectiveAt time.Time

	Actor ActorRef

	Sources    []SourceRef
	Inputs     []Ref
	Causes     []Ref
	Effects    []Effect
	Supersedes []EventID
	Reason     string

	Ontologies []OntologyRef
	Invocation *InvocationRef

	PreviousHash string
	Hash         string
}

// Part is a current projection. Authoritative changes live in events.
type Part struct {
	ID       PartID
	TicketID TicketID

	Name        string
	DisplayName string
	ParentID    *PartID

	Value     *Value
	ValueKind ValueKind

	Types []string

	CreatedBy   EventID
	CurrentFrom EventID
	RetractedBy *EventID

	Sources []SourceRef
}

type PartContainment struct {
	TicketID TicketID
	ChildID  PartID
	ParentID *PartID

	AttachedBy  EventID
	DetachedBy  *EventID
	EffectiveAt time.Time
}

type Link struct {
	ID       LinkID
	From     Ref
	Relation string
	To       Ref

	Origin string

	CreatedBy   EventID
	RetractedBy *EventID
}

type CodeRef struct {
	Repository string
	Commit     string
	Path       string

	StartLine *int
	EndLine   *int
	StartByte *int
	EndByte   *int

	Symbol    *string
	Package   *string
	ASTNode   *string
	CFGNode   *string
	DFGNode   *string
	GraphNode *string
}

type GitRef struct {
	Repository string

	Commit *string
	Base   *string
	Head   *string
	Branch *string
	Tag    *string
	PR     *string
	Diff   *string
}

type Evidence struct {
	Ref       Ref
	Kind      string
	ClaimRefs []Ref
	Sources   []SourceRef
	ProducedBy Ref
}

type VerificationResult struct {
	Ref       Ref
	Claim     Ref
	Method    string
	Status    string
	Evidence  []Ref
	Evaluator Ref
	Ontology  OntologyRef
}

type ProcessorInvocation struct {
	ID          InvocationRef
	Processor   string
	Version     string
	CodeHash    string
	ConfigHash  string

	Inputs      []Ref
	InputEvents []EventID
	Outputs     []Ref

	Capabilities []string

	StartedAt  time.Time
	EndedAt    time.Time
	Status     string
	Diagnostic string
}

// Projection is the derived current state for one ticket.
type Projection struct {
	TicketID TicketID

	Parts map[PartID]*Part
	Links map[LinkID]*Link
	Paths map[string]PartID

	EffectiveAt time.Time
	KnownAt     time.Time
}

// ResolvedPartPath is the result of resolving a human-readable part path.
type ResolvedPartPath struct {
	PartID     PartID
	TicketID   TicketID
	Segments   []string
	EffectiveAt time.Time
	KnownAt     time.Time
}
