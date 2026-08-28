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
	ValueKindText         ValueKind = "text"
	ValueKindMarkdown     ValueKind = "markdown"
	ValueKindScalar       ValueKind = "scalar"
	ValueKindStatus       ValueKind = "status"
	ValueKindPriority     ValueKind = "priority"
	ValueKindMap          ValueKind = "map"
	ValueKindList         ValueKind = "list"
	ValueKindRef          ValueKind = "ref"
	ValueKindCodeRef      ValueKind = "code-ref"
	ValueKindGitRef       ValueKind = "git-ref"
	ValueKindEvidence     ValueKind = "evidence"
	ValueKindVerification ValueKind = "verification"
	ValueKindJSON         ValueKind = "json"
	ValueKindArtifact     ValueKind = "artifact"
	ValueKindExternalRef  ValueKind = "external-ref"
	ValueKindAnnotation   ValueKind = "annotation"
	// Media kinds are durable revision-2 value kinds. They carry a URI or a
	// MediaDescriptor in Value.Data; they do not imply that every consumer can
	// display or play the referenced media.
	ValueKindImage          ValueKind = "image"
	ValueKindVideo          ValueKind = "video"
	ValueKindAudio          ValueKind = "audio"
	ValueKindEmbed          ValueKind = "embed"
	ValueKindInlineSequence ValueKind = "inline-sequence"
)

var builtInValueKinds = []ValueKind{
	ValueKindText, ValueKindMarkdown, ValueKindScalar, ValueKindStatus,
	ValueKindPriority, ValueKindMap, ValueKindList, ValueKindRef,
	ValueKindCodeRef, ValueKindGitRef, ValueKindEvidence, ValueKindVerification,
	ValueKindJSON, ValueKindArtifact, ValueKindExternalRef, ValueKindAnnotation, ValueKindImage,
	ValueKindVideo, ValueKindAudio, ValueKindEmbed, ValueKindInlineSequence,
}

// AllBuiltInValueKinds is the durable built-in value vocabulary. Compatibility
// fixtures use it as a completeness guard whenever a new kind is registered.
func AllBuiltInValueKinds() []ValueKind {
	return append([]ValueKind(nil), builtInValueKinds...)
}

var builtInRefKinds = []Kind{
	KindTicket, KindPart, KindEvent, KindProject, KindGroup, KindRun,
	KindCode, KindGit, KindArtifact,
}

// AllBuiltInRefKinds returns every durable built-in Ref kind.
func AllBuiltInRefKinds() []Kind {
	return append([]Kind(nil), builtInRefKinds...)
}

// Value is the payload stored on a part. Retracted is a projection flag, not
// the mechanism for removing history; retraction is represented by events.
type Value struct {
	Kind      ValueKind
	Text      string
	Data      any
	List      []string
	Ref       *Ref
	Retracted bool
	// OrderKey is optional containment metadata. It is omitted from pre-order-key
	// event JSON and does not change hashes for events that do not use ordered
	// children.
	OrderKey string `json:"OrderKey,omitempty"`
}

// ContainmentDescriptor documents the structured meaning of order metadata
// when callers exchange attach/move payloads. Value.OrderKey is the compact
// wire field used by events so typed child values can keep Value.Data.
type ContainmentDescriptor struct {
	OrderKey string `json:"order_key,omitempty"`
}

type Operation string

const (
	OpCreateEntity       Operation = "create-entity"
	OpCreatePart         Operation = "create-part"
	OpSetValue           Operation = "set-value"
	OpAddValue           Operation = "add-value"
	OpRetractValue       Operation = "retract-value"
	OpRenamePart         Operation = "rename-part"
	OpMovePart           Operation = "move-part"
	OpAttachChild        Operation = "attach-child"
	OpDetachChild        Operation = "detach-child"
	OpRetractSubtree     Operation = "retract-subtree"
	OpRestorePart        Operation = "restore-part"
	OpAssertLink         Operation = "assert-link"
	OpRetractLink        Operation = "retract-link"
	OpAssignOntology     Operation = "assign-ontology"
	OpRemoveOntology     Operation = "remove-ontology"
	OpJoinScope          Operation = "join-scope"
	OpLeaveScope         Operation = "leave-scope"
	OpObserveEffect      Operation = "observe-effect"
	OpAttachEvidence     Operation = "attach-evidence"
	OpRecordVerification Operation = "record-verification"
	OpSupersedeEvent     Operation = "supersede-event"
)

type OntologyRef struct {
	ID      string
	Version string
	Hash    string
}

type InvocationRef struct {
	ID          string    `json:"ID"`
	Plugin      string    `json:"Plugin,omitempty"`
	Version     string    `json:"Version,omitempty"`
	CodeHash    string    `json:"CodeHash,omitempty"`
	RequestedBy *ActorRef `json:"RequestedBy,omitempty"`
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

	Sources         []SourceRef
	OrderKey        string
	CreatedSequence uint64
}

type PartContainment struct {
	TicketID TicketID
	ChildID  PartID
	ParentID *PartID

	AttachedBy  EventID
	DetachedBy  *EventID
	EffectiveAt time.Time
	OrderKey    string
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
	Repository string `json:"Repository"`
	Commit     string `json:"Commit"`
	Path       string `json:"Path"`

	StartLine *int `json:"StartLine,omitempty"`
	EndLine   *int `json:"EndLine,omitempty"`
	StartByte *int `json:"StartByte,omitempty"`
	EndByte   *int `json:"EndByte,omitempty"`

	Symbol    *string `json:"Symbol,omitempty"`
	Package   *string `json:"Package,omitempty"`
	ASTNode   *string `json:"ASTNode,omitempty"`
	CFGNode   *string `json:"CFGNode,omitempty"`
	DFGNode   *string `json:"DFGNode,omitempty"`
	GraphNode *string `json:"GraphNode,omitempty"`
}

type GitRef struct {
	Repository string `json:"Repository"`

	Commit *string `json:"Commit,omitempty"`
	Base   *string `json:"Base,omitempty"`
	Head   *string `json:"Head,omitempty"`
	Branch *string `json:"Branch,omitempty"`
	Tag    *string `json:"Tag,omitempty"`
	PR     *string `json:"PR,omitempty"`
	Diff   *string `json:"Diff,omitempty"`
}

// ArtifactDescriptor carries only immutable artifact identity and metadata.
// The bytes live in an artifact.Store and are never embedded in a Part or
// event payload.
type ArtifactDescriptor struct {
	Ref       Ref    `json:"Ref"`
	MediaType string `json:"MediaType,omitempty"`
	Size      int64  `json:"Size"`
}

type Evidence struct {
	Ref        Ref
	Kind       string
	ClaimRefs  []Ref
	Sources    []SourceRef
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
	ID         InvocationRef
	Processor  string
	Version    string
	CodeHash   string
	ConfigHash string

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
	Stream   Ref

	Parts map[PartID]*Part
	Links map[LinkID]*Link
	Paths map[string]PartID

	EffectiveAt time.Time
	KnownAt     time.Time
}

// ResolvedPartPath is the result of resolving a human-readable part path.
type ResolvedPartPath struct {
	PartID      PartID
	TicketID    TicketID
	Segments    []string
	EffectiveAt time.Time
	KnownAt     time.Time
}
