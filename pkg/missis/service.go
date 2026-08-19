package missis

import (
	"context"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
)

// Service is the application-layer contract implemented by
// internal/application and consumed through the Client facade. It is the
// single New/Show/Set service shared by the CLI, SDK, and TUI.
type Service interface {
	Close() error
	Store() *store.Store
	StorePath() string
	SuppliedPath() string
	DiscoverySource() DiscoverySource
	StoreID() (string, error)
	HeadHash() (string, error)
	EventCount() (int64, error)
	SchemaVersion() (string, error)
	CheckConsistency(ctx context.Context) error
	Backup(ctx context.Context, dst string) error
	SequenceGaps(ctx context.Context) ([]store.SequenceGap, error)
	RepairSequenceGaps(ctx context.Context) error
	RebuildProjection(ctx context.Context) error
	LoadEvents(ctx context.Context) ([]model.Event, error)
	LoadLinkEvents(ctx context.Context) ([]model.Event, error)
	LoadTicketEvents(ctx context.Context, ticketID model.TicketID) ([]model.Event, error)
	CurrentProjection(ctx context.Context, ticketID model.TicketID, effectiveAt time.Time) (*model.Projection, error)
	BitemporalProjection(ctx context.Context, ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error)
	GetEventByAlias(ctx context.Context, alias string) (model.Event, error)
	ListTickets(ctx context.Context, effectiveAt time.Time) ([]store.TicketSummary, error)
	AppendBatch(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []store.Precondition, result any) (store.AppendOutcome, error)
	AppendTicketBatch(ctx context.Context, events []model.Event, idempotencyKey string, result any) (store.AppendOutcome, uint64, error)
	LookupTicketAlias(ctx context.Context, ticketID model.TicketID) (uint64, error)
	LookupIdempotency(key string, result any) (bool, error)
	UpdateIdempotencyResult(key string, result any) error

	NewTicket(ctx context.Context, req RequestContext, opts NewTicketOptions) (NewTicketResult, error)
	NewEntity(ctx context.Context, req RequestContext, opts EntityOptions) (EntityResult, error)
	ImportMarkdown(ctx context.Context, req RequestContext, opts ImportOptions) (NewTicketResult, error)
	ReimportMarkdown(ctx context.Context, req RequestContext, opts ImportOptions) (ImportResult, error)
	ListTicketSummaries(ctx context.Context, effectiveAt time.Time) ([]TicketSummary, error)
	ListEntities(ctx context.Context, kind model.Kind, filter ListFilter) ([]EntitySummary, error)
	ShowTicket(ctx context.Context, ref string, opts ShowOptions) (TicketProjection, error)
	ShowEntity(ctx context.Context, ref string, opts ShowOptions) (TicketProjection, error)
	ShowHistory(ctx context.Context, ref string, opts HistoryOptions) ([]EventView, error)
	ShowEvent(ctx context.Context, alias string) (EventView, error)
	ShowReferences(ctx context.Context, ref string, opts ShowOptions) ([]LinkView, error)
	ShowLineage(ctx context.Context, ref string, opts LineageOptions) ([]LineageEdge, error)
	ResolveAnyRef(ctx context.Context, ref string, effectiveAt time.Time) (string, error)
	Search(ctx context.Context, opts SearchOptions) ([]TicketSummary, error)
	ListTicketsFiltered(ctx context.Context, filter ListFilter) ([]TicketSummary, error)
	Set(ctx context.Context, req RequestContext, mutation Mutation) (SetResult, error)
	SetLink(ctx context.Context, req RequestContext, opts LinkOptions) (SetResult, error)
	Manifest(ctx context.Context) (ManifestInfo, error)
	BackupTo(ctx context.Context, dst string) error
	Restore(ctx context.Context, backupPath, dst string) error
	VerifyRestore(ctx context.Context, backupPath string, expect ManifestInfo) error
}
