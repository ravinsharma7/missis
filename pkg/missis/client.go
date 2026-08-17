package missis

import (
	"context"
	"time"

	"github.com/ravinsharma7/missis/implementation/model"
	"github.com/ravinsharma7/missis/implementation/store"
)

type HealthInfo struct {
	Status     string `json:"status"`
	StoreID    string `json:"store_id"`
	HeadHash   string `json:"head_hash"`
	EventCount int64  `json:"event_count"`
	Version    string `json:"version"`
	Commit     string `json:"commit"`
}

type Client struct {
	store    *store.Store
	path     string
	supplied string
	source   DiscoverySource
}

func Open(storeFlag string) (*Client, error) {
	resolved, err := ResolveStore(storeFlag)
	if err != nil {
		return nil, err
	}
	return openResolved(resolved)
}

func OpenPath(path string) (*Client, error) {
	return openResolved(ResolvedStore{Path: path, Supplied: path, Source: DiscoveryFlag})
}

func openResolved(resolved ResolvedStore) (*Client, error) {
	s, err := store.Open(resolved.Path)
	if err != nil {
		return nil, err
	}
	return &Client{store: s, path: resolved.Path, supplied: resolved.Supplied, source: resolved.Source}, nil
}

func (c *Client) Close() error {
	return c.store.Close()
}

func (c *Client) Store() *store.Store {
	return c.store
}

// StorePath returns the resolved absolute store path.
func (c *Client) StorePath() string {
	return c.path
}

// SuppliedPath returns the store path as supplied by the discovery source.
func (c *Client) SuppliedPath() string {
	return c.supplied
}

// DiscoverySource returns where the store path came from.
func (c *Client) DiscoverySource() DiscoverySource {
	return c.source
}

func (c *Client) StoreID() (string, error) {
	return c.store.StoreID()
}

func (c *Client) HeadHash() (string, error) {
	return c.store.HeadHash()
}

func (c *Client) EventCount() (int64, error) {
	return c.store.EventCount()
}

func (c *Client) SchemaVersion() (string, error) {
	return c.store.SchemaVersion()
}

func (c *Client) CheckConsistency(ctx context.Context) error {
	return c.store.CheckConsistency()
}

func (c *Client) Backup(ctx context.Context, dst string) error {
	return c.store.Backup(dst)
}

func (c *Client) SequenceGaps(ctx context.Context) ([]store.SequenceGap, error) {
	return c.store.SequenceGaps()
}

func (c *Client) RepairSequenceGaps(ctx context.Context) error {
	return c.store.RepairSequenceGaps()
}

func (c *Client) LoadEvents(ctx context.Context) ([]model.Event, error) {
	return c.store.LoadEvents()
}

func (c *Client) LoadLinkEvents(ctx context.Context) ([]model.Event, error) {
	return c.store.LoadLinkEvents()
}

func (c *Client) LoadTicketEvents(ctx context.Context, ticketID model.TicketID) ([]model.Event, error) {
	return c.store.LoadTicketEvents(ticketID)
}

func (c *Client) CurrentProjection(ctx context.Context, ticketID model.TicketID, effectiveAt time.Time) (*model.Projection, error) {
	return c.store.CurrentProjection(ticketID, effectiveAt)
}

func (c *Client) BitemporalProjection(ctx context.Context, ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return c.store.BitemporalProjection(ticketID, effectiveAt, knownAt)
}

func (c *Client) GetEventByAlias(ctx context.Context, alias string) (model.Event, error) {
	return c.store.GetEventByAlias(alias)
}

func (c *Client) ListTickets(ctx context.Context, effectiveAt time.Time) ([]store.TicketSummary, error) {
	return c.store.ListTickets(effectiveAt)
}

func (c *Client) AppendBatch(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []store.Precondition, result any) (store.AppendOutcome, error) {
	return c.store.AppendBatch(events, idempotencyKey, preconditions, result)
}

func (c *Client) AppendTicketBatch(ctx context.Context, events []model.Event, idempotencyKey string, result any) (store.AppendOutcome, uint64, error) {
	return c.store.AppendTicketBatch(events, idempotencyKey, result)
}

func (c *Client) LookupTicketAlias(ticketID model.TicketID) (uint64, error) {
	return c.store.LookupTicketAlias(ticketID)
}

func (c *Client) LookupIdempotency(key string, result any) (bool, error) {
	return c.store.LookupIdempotency(key, result)
}

func (c *Client) UpdateIdempotencyResult(key string, result any) error {
	return c.store.UpdateIdempotencyResult(key, result)
}
