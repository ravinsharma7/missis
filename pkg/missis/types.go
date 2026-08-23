package missis

import (
	"io"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

// RequestContext carries the per-request provenance and concurrency inputs.
// Defaults (actor human/local, times from the service clock) are applied in
// exactly one place: the application service.
type RequestContext struct {
	Actor          string
	EffectiveAt    time.Time
	KnownAt        time.Time
	IdempotencyKey string
	IfCurrent      string
	Because        string
}

// Clock abstracts time so the service can be tested deterministically.
type Clock interface {
	Now() time.Time
}

// ErrorKind classifies domain errors independently of CLI exit codes.
type ErrorKind string

const (
	ErrInvalidInput ErrorKind = "invalid_input"
	ErrNotFound     ErrorKind = "not_found"
	ErrValidation   ErrorKind = "validation_failed"
	ErrConflict     ErrorKind = "concurrency_conflict"
	ErrStorage      ErrorKind = "storage_failure"
)

// DomainError is the stable, typed error contract returned by the service.
type DomainError struct {
	Kind    ErrorKind
	Message string
}

func (e *DomainError) Error() string {
	return e.Message
}

// Mutation is the validated tagged union for every Set operation. The CLI
// parser builds exactly one of these; invalid flag combinations are rejected
// before they reach the service.
type Mutation interface {
	isMutation()
}

type SetValue struct {
	Target string
	Value  string
	Kind   model.ValueKind
	Reason string
}

// SetValueData writes an explicitly structured value. It is the API path for
// CodeRef, GitRef, media, artifact descriptors, and plugin-owned payloads;
// the application still validates Kind and the registered schema before
// appending the event.
type SetValueData struct {
	Target string
	Data   any
	Kind   model.ValueKind
	Reason string
}

type AddValue struct {
	Target string
	Value  string
	Reason string
}

type RetractValue struct {
	Target string
	Reason string
}

type RetractSubtree struct {
	Target string
	Reason string
}

type RenamePart struct {
	Target string
	Name   string
	Reason string
}

type MovePart struct {
	Target string
	Parent string
	// Before and After are neighboring Part references. The core resolves
	// them and assigns the opaque order key; clients never calculate keys.
	Before string
	After  string
	Reason string
}

type SupersedeEvent struct {
	Target     string
	Value      string
	Kind       model.ValueKind
	Supersedes string
	Reason     string
}

func (SetValue) isMutation()       {}
func (SetValueData) isMutation()   {}
func (AddValue) isMutation()       {}
func (RetractValue) isMutation()   {}
func (RetractSubtree) isMutation() {}
func (RenamePart) isMutation()     {}
func (MovePart) isMutation()       {}
func (SupersedeEvent) isMutation() {}

// ----- result types -----

type TicketSummary struct {
	Ref        string
	ID         string
	Title      string
	Status     string
	RecordedAt time.Time
}

type EntitySummary struct {
	Ref        string    `json:"ref"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	RecordedAt time.Time `json:"recorded_at"`
}

type PartView struct {
	ID             string
	Path           string
	Value          any
	ValueKind      string
	DeclaredSchema string
	ParentID       any
	CreatedBy      string
	Name           string
	DisplayName    string
	OrderKey       string
}

type TicketProjection struct {
	Ref        string
	ID         string
	Title      string
	Status     string
	RecordedAt time.Time
	Parts      map[string]PartView
	PartOrder  []string
}

type EventView struct {
	ID          string
	Alias       string
	Sequence    uint64
	Operation   string
	Target      string
	Value       any
	RecordedAt  time.Time
	EffectiveAt time.Time
	Actor       string
	Reason      string
}

type LinkView struct {
	From       string              `json:"from"`
	Relation   string              `json:"relation"`
	To         string              `json:"to"`
	Direction  string              `json:"direction"`
	Origin     string              `json:"origin"`
	CreatedBy  string              `json:"created_by"`
	Assertions []LinkAssertionView `json:"assertions,omitempty"`
}

// LinkAssertionView is one piece of evidence for a visible link relation
// (ticket #66). A relation is visible while at least one assertion is active.
type LinkAssertionView struct {
	CreatedBy string   `json:"created_by"`
	Actor     string   `json:"actor"`
	Sources   []string `json:"sources"`
}

type LineageEdge struct {
	From      string
	Relation  string
	To        string
	Direction string
	Depth     int
	Origin    string
	CreatedBy string
}

type NewTicketResult struct {
	Ref         string   `json:"ref"`
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Project     *string  `json:"project"`
	RecordedAt  string   `json:"recorded_at"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

type EntityResult struct {
	Ref        string `json:"ref"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	RecordedAt string `json:"recorded_at"`
}

type SetResult struct {
	Ref       string `json:"ref"`
	Event     string `json:"event"`
	Operation string `json:"operation"`
	Value     any    `json:"value"`
	Warning   string `json:"warning,omitempty"`
}

type ImportResult struct {
	Ref         string   `json:"ref"`
	Event       string   `json:"event"`
	Operation   string   `json:"operation"`
	Value       int      `json:"value"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// ----- option types -----

type NewTicketOptions struct {
	Title    string
	Project  string
	Priority string
	Types    []string
	Tags     []string
}

type EntityOptions struct {
	Kind  string
	ID    string
	Title string
}

type ImportOptions struct {
	Ref      string
	Title    string
	Content  string
	Artifact string
	Project  string
}

// IngestOptions selects a target Part stream and supplies immutable content.
// Plugin selection is based on operation and media metadata, not a plugin ID.
type IngestOptions struct {
	Operation            string
	Target               string
	Path                 []string
	MediaType            string
	SourceName           string
	LegacySource         string
	ExcludeTopLevelTitle bool
	DeclaredSchema       string
	Capabilities         []string
	Content              io.Reader
}

type IngestResult struct {
	Ref         string   `json:"ref"`
	Artifact    string   `json:"artifact"`
	Event       string   `json:"event,omitempty"`
	Operation   string   `json:"operation"`
	Value       int      `json:"value"`
	Plugin      string   `json:"plugin"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

type ShowOptions struct {
	EffectiveAt time.Time
	KnownAt     time.Time
}

type HistoryOptions struct {
	EffectiveAt time.Time
	KnownAt     time.Time
	Since       time.Time
	PartPath    []string
}

type LinkOptions struct {
	Ref       string
	Relation  string
	Target    string
	Add       bool
	Retract   bool
	Reason    string
	Assertion string
}

// LinkBatchItem describes one additive link in an atomic link batch. Source
// and target accept the same references as LinkOptions. MoveFrom is only used
// for a has-home move and identifies the currently asserted project.
type LinkBatchItem struct {
	Source   string
	Relation string
	Target   string
	MoveFrom string
	Reason   string
}

// LinkBatchOptions is the input to ApplyLinkBatch. The operation is additive
// from the caller's perspective: active duplicate triples are skipped, while
// previously retracted assertions may be added again as new evidence.
type LinkBatchOptions struct {
	Items []LinkBatchItem
}

// LinkBatchResult reports the links appended and the active duplicate items
// that were intentionally skipped.
type LinkBatchResult struct {
	Added   []SetResult
	Skipped []string
}

type MoveLinkOptions struct {
	Relation  string
	From      string
	To        string
	Target    string
	Reason    string
	IfCurrent string
}

// ScopeOptions describes a Phase 4 scope membership transition (ticket #74):
// an entity (ticket, project, or group) joins or leaves a project/group scope
// as a member-of relation. Assertion targets a specific membership assertion
// on leave.
type ScopeOptions struct {
	Entity    string
	Scope     string
	Reason    string
	Assertion string
}

type LineageOptions struct {
	Direction   string
	Depth       int
	Relations   []string
	EffectiveAt time.Time
	KnownAt     time.Time
}

type SearchOptions struct {
	Query    string
	Status   string
	Projects []string
	Groups   []string
	// Unscoped selects tickets that match neither a project nor a group view.
	// It is mutually exclusive with project and group inputs.
	Unscoped    bool
	Type        string
	Tag         string
	EffectiveAt time.Time
	KnownAt     time.Time
}

type ListFilter struct {
	// Projects and Groups are the typed multi-scope filter inputs. Values are
	// unioned within each kind and intersected when both kinds are present.
	Projects []string
	Groups   []string
	// Unscoped selects tickets that match neither a project nor a group view.
	// It is mutually exclusive with project and group inputs.
	Unscoped    bool
	Status      string
	Type        string
	Tag         string
	Query       string
	EffectiveAt time.Time
	KnownAt     time.Time
}

// ManifestInfo is the portable store fingerprint used for backup naming and
// restore verification.
type ManifestInfo struct {
	SchemaVersion string `json:"schema_version"`
	StoreID       string `json:"store_id"`
	HeadHash      string `json:"head_hash"`
	EventCount    int64  `json:"event_count"`
}

// ----- creation workflows -----
