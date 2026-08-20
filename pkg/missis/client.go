package missis

import (
	"context"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
)

// Client is the thin public facade over the application Service. It adds no
// behavior: the CLI, TUI, and tools construct a Service (composition root)
// and wrap it with NewClient.
type Client struct {
	service Service
}

// NewClient wraps an application-layer Service in the public facade.
func NewClient(service Service) *Client {
	return &Client{service: service}
}

func (c *Client) Close() error {
	return c.service.Close()
}

func (c *Client) Store() *store.Store {
	return c.service.Store()
}

func (c *Client) StorePath() string {
	return c.service.StorePath()
}

func (c *Client) SuppliedPath() string {
	return c.service.SuppliedPath()
}

func (c *Client) DiscoverySource() DiscoverySource {
	return c.service.DiscoverySource()
}

func (c *Client) StoreID() (string, error) {
	return c.service.StoreID()
}

func (c *Client) StoreIDContext(ctx context.Context) (string, error) {
	return c.service.StoreIDContext(ctx)
}

func (c *Client) HeadHash() (string, error) {
	return c.service.HeadHash()
}

func (c *Client) HeadHashContext(ctx context.Context) (string, error) {
	return c.service.HeadHashContext(ctx)
}

func (c *Client) EventCount() (int64, error) {
	return c.service.EventCount()
}

func (c *Client) EventCountContext(ctx context.Context) (int64, error) {
	return c.service.EventCountContext(ctx)
}

func (c *Client) SchemaVersion() (string, error) {
	return c.service.SchemaVersion()
}

func (c *Client) SchemaVersionContext(ctx context.Context) (string, error) {
	return c.service.SchemaVersionContext(ctx)
}

func (c *Client) CheckConsistency(ctx context.Context) error {
	return c.service.CheckConsistency(ctx)
}

func (c *Client) Backup(ctx context.Context, dst string) error {
	return c.service.Backup(ctx, dst)
}

func (c *Client) SequenceGaps(ctx context.Context) ([]store.SequenceGap, error) {
	return c.service.SequenceGaps(ctx)
}

func (c *Client) RepairSequenceGaps(ctx context.Context) error {
	return c.service.RepairSequenceGaps(ctx)
}

func (c *Client) RebuildProjection(ctx context.Context) error {
	return c.service.RebuildProjection(ctx)
}

func (c *Client) LoadEvents(ctx context.Context) ([]model.Event, error) {
	return c.service.LoadEvents(ctx)
}

func (c *Client) LoadLinkEvents(ctx context.Context) ([]model.Event, error) {
	return c.service.LoadLinkEvents(ctx)
}

func (c *Client) LoadTicketEvents(ctx context.Context, ticketID model.TicketID) ([]model.Event, error) {
	return c.service.LoadTicketEvents(ctx, ticketID)
}

func (c *Client) CurrentProjection(ctx context.Context, ticketID model.TicketID, effectiveAt time.Time) (*model.Projection, error) {
	return c.service.CurrentProjection(ctx, ticketID, effectiveAt)
}

func (c *Client) BitemporalProjection(ctx context.Context, ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return c.service.BitemporalProjection(ctx, ticketID, effectiveAt, knownAt)
}

func (c *Client) GetEventByAlias(ctx context.Context, alias string) (model.Event, error) {
	return c.service.GetEventByAlias(ctx, alias)
}

func (c *Client) ListTickets(ctx context.Context, effectiveAt time.Time) ([]store.TicketSummary, error) {
	return c.service.ListTickets(ctx, effectiveAt)
}

func (c *Client) AppendBatch(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []store.Precondition, result any) (store.AppendOutcome, error) {
	return c.service.AppendBatch(ctx, events, idempotencyKey, preconditions, result)
}

func (c *Client) AppendTicketBatch(ctx context.Context, events []model.Event, idempotencyKey string, result any) (store.AppendOutcome, uint64, error) {
	return c.service.AppendTicketBatch(ctx, events, idempotencyKey, result)
}

func (c *Client) LookupTicketAlias(ctx context.Context, ticketID model.TicketID) (uint64, error) {
	return c.service.LookupTicketAlias(ctx, ticketID)
}

func (c *Client) LookupIdempotency(key string, result any) (bool, error) {
	return c.service.LookupIdempotency(key, result)
}

func (c *Client) LookupIdempotencyContext(ctx context.Context, key string, result any) (bool, error) {
	return c.service.LookupIdempotencyContext(ctx, key, result)
}

func (c *Client) UpdateIdempotencyResult(key string, result any) error {
	return c.service.UpdateIdempotencyResult(key, result)
}

func (c *Client) UpdateIdempotencyResultContext(ctx context.Context, key string, result any) error {
	return c.service.UpdateIdempotencyResultContext(ctx, key, result)
}

func (c *Client) NewTicket(ctx context.Context, req RequestContext, opts NewTicketOptions) (NewTicketResult, error) {
	return c.service.NewTicket(ctx, req, opts)
}

func (c *Client) NewEntity(ctx context.Context, req RequestContext, opts EntityOptions) (EntityResult, error) {
	return c.service.NewEntity(ctx, req, opts)
}

func (c *Client) ImportMarkdown(ctx context.Context, req RequestContext, opts ImportOptions) (NewTicketResult, error) {
	return c.service.ImportMarkdown(ctx, req, opts)
}

func (c *Client) ReimportMarkdown(ctx context.Context, req RequestContext, opts ImportOptions) (ImportResult, error) {
	return c.service.ReimportMarkdown(ctx, req, opts)
}

func (c *Client) ListTicketSummaries(ctx context.Context, effectiveAt time.Time) ([]TicketSummary, error) {
	return c.service.ListTicketSummaries(ctx, effectiveAt)
}

func (c *Client) ListEntities(ctx context.Context, kind model.Kind, filter ListFilter) ([]EntitySummary, error) {
	return c.service.ListEntities(ctx, kind, filter)
}

func (c *Client) ShowTicket(ctx context.Context, ref string, opts ShowOptions) (TicketProjection, error) {
	return c.service.ShowTicket(ctx, ref, opts)
}

func (c *Client) ShowEntity(ctx context.Context, ref string, opts ShowOptions) (TicketProjection, error) {
	return c.service.ShowEntity(ctx, ref, opts)
}

func (c *Client) ShowHistory(ctx context.Context, ref string, opts HistoryOptions) ([]EventView, error) {
	return c.service.ShowHistory(ctx, ref, opts)
}

func (c *Client) ShowEvent(ctx context.Context, alias string) (EventView, error) {
	return c.service.ShowEvent(ctx, alias)
}

func (c *Client) ShowReferences(ctx context.Context, ref string, opts ShowOptions) ([]LinkView, error) {
	return c.service.ShowReferences(ctx, ref, opts)
}

func (c *Client) ShowLineage(ctx context.Context, ref string, opts LineageOptions) ([]LineageEdge, error) {
	return c.service.ShowLineage(ctx, ref, opts)
}

func (c *Client) ResolveAnyRef(ctx context.Context, ref string, effectiveAt time.Time) (string, error) {
	return c.service.ResolveAnyRef(ctx, ref, effectiveAt)
}

func (c *Client) Search(ctx context.Context, opts SearchOptions) ([]TicketSummary, error) {
	return c.service.Search(ctx, opts)
}

func (c *Client) ListTicketsFiltered(ctx context.Context, filter ListFilter) ([]TicketSummary, error) {
	return c.service.ListTicketsFiltered(ctx, filter)
}

func (c *Client) CountTicketsFiltered(ctx context.Context, filter ListFilter) (int, error) {
	return c.service.CountTicketsFiltered(ctx, filter)
}

func (c *Client) Set(ctx context.Context, req RequestContext, mutation Mutation) (SetResult, error) {
	return c.service.Set(ctx, req, mutation)
}

// RetractPart is a convenience for the common retraction mutations.
func (c *Client) RetractPart(ctx context.Context, req RequestContext, target, reason string, recursive bool) (SetResult, error) {
	if recursive {
		return c.service.Set(ctx, req, RetractSubtree{Target: target, Reason: reason})
	}
	return c.service.Set(ctx, req, RetractValue{Target: target, Reason: reason})
}

func (c *Client) SetLink(ctx context.Context, req RequestContext, opts LinkOptions) (SetResult, error) {
	return c.service.SetLink(ctx, req, opts)
}

func (c *Client) ApplyLinkBatch(ctx context.Context, req RequestContext, opts LinkBatchOptions) (LinkBatchResult, error) {
	return c.service.ApplyLinkBatch(ctx, req, opts)
}

func (c *Client) MoveLink(ctx context.Context, req RequestContext, opts MoveLinkOptions) (SetResult, error) {
	return c.service.MoveLink(ctx, req, opts)
}

func (c *Client) JoinScope(ctx context.Context, req RequestContext, opts ScopeOptions) (SetResult, error) {
	return c.service.JoinScope(ctx, req, opts)
}

func (c *Client) LeaveScope(ctx context.Context, req RequestContext, opts ScopeOptions) (SetResult, error) {
	return c.service.LeaveScope(ctx, req, opts)
}

// MoveHome moves a ticket's has-home link from one project to another in one
// atomic batch. It is a convenience for MoveLink with relation has-home.
func (c *Client) MoveHome(ctx context.Context, req RequestContext, ticketRef, fromProject, toProject, reason string) (SetResult, error) {
	return c.service.MoveLink(ctx, req, MoveLinkOptions{
		Relation: model.RelationHasHome,
		From:     "project:" + fromProject,
		To:       "project:" + toProject,
		Target:   ticketRef,
		Reason:   reason,
	})
}

func (c *Client) Manifest(ctx context.Context) (ManifestInfo, error) {
	return c.service.Manifest(ctx)
}

func (c *Client) BackupTo(ctx context.Context, dst string) error {
	return c.service.BackupTo(ctx, dst)
}

func (c *Client) Restore(ctx context.Context, backupPath, dst string) error {
	return c.service.Restore(ctx, backupPath, dst)
}

func (c *Client) VerifyRestore(ctx context.Context, backupPath string, expect ManifestInfo) error {
	return c.service.VerifyRestore(ctx, backupPath, expect)
}
