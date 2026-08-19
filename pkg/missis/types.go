package missis

import (
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
}

type TicketProjection struct {
	Ref        string
	ID         string
	Title      string
	Status     string
	RecordedAt time.Time
	Parts      map[string]PartView
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
	From      string
	Relation  string
	To        string
	Direction string
	Origin    string
	CreatedBy string
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
	Ref        string  `json:"ref"`
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	Project    *string `json:"project"`
	RecordedAt string  `json:"recorded_at"`
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
	Ref       string `json:"ref"`
	Event     string `json:"event"`
	Operation string `json:"operation"`
	Value     int    `json:"value"`
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
	Ref      string
	Relation string
	Target   string
	Add      bool
	Retract  bool
	Reason   string
}

type LineageOptions struct {
	Direction   string
	Depth       int
	Relations   []string
	EffectiveAt time.Time
	KnownAt     time.Time
}

type SearchOptions struct {
	Query       string
	Status      string
	Project     string
	Group       string
	Type        string
	Tag         string
	EffectiveAt time.Time
	KnownAt     time.Time
}

type ListFilter struct {
	Project     string
	Group       string
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
