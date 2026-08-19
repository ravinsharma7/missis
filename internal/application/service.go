package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// Service is the single application layer behind the CLI, SDK, and TUI. It
// owns store access and all New/Show/Set orchestration; the public facade in
// pkg/missis delegates to it.
type Service struct {
	store    *store.Store
	path     string
	supplied string
	source   missis.DiscoverySource
	clock    missis.Clock
}

// Open resolves the store (flag/env/marker/default) and opens the service.
func Open(storeFlag string) (*Service, error) {
	return OpenWithClock(storeFlag, realClock{})
}

// OpenWithClock opens a service with an explicit clock (tests).
func OpenWithClock(storeFlag string, clock missis.Clock) (*Service, error) {
	resolved, err := missis.ResolveStore(storeFlag)
	if err != nil {
		return nil, err
	}
	return openResolved(resolved, clock)
}

// OpenPath opens a service at an explicit store path.
func OpenPath(path string) (*Service, error) {
	return OpenPathWithClock(path, realClock{})
}

// OpenPathWithClock opens a service at an explicit path with a test clock.
func OpenPathWithClock(path string, clock missis.Clock) (*Service, error) {
	return openResolved(missis.ResolvedStore{Path: path, Supplied: path, Source: missis.DiscoveryFlag}, clock)
}

func openResolved(resolved missis.ResolvedStore, clock missis.Clock) (*Service, error) {
	var (
		s   *store.Store
		err error
	)
	if diag := storeDiagnosticsFromEnv(); diag != nil {
		s, err = store.OpenWithDiag(resolved.Path, diag)
	} else {
		s, err = store.Open(resolved.Path)
	}
	if err != nil {
		return nil, err
	}
	return &Service{
		store:    s,
		path:     resolved.Path,
		supplied: resolved.Supplied,
		source:   resolved.Source,
		clock:    clock,
	}, nil
}

// storeDiagnosticsFromEnv wires append-path diagnostics (ticket #65).
// MISSIS_STORE_DIAG is a path to a JSON-lines file, or one of "1"/"true"/
// "stderr" to write to stderr. Unset, "0", or "false" disables. CI sets it
// unconditionally so every run captures structured evidence; the store itself
// stays free of environment reads.
func storeDiagnosticsFromEnv() store.Diagnostics {
	value := os.Getenv("MISSIS_STORE_DIAG")
	switch value {
	case "", "0", "false":
		return nil
	case "1", "true", "stderr":
		return store.NewJSONLinesDiagnostics(os.Stderr)
	}
	if err := os.MkdirAll(filepath.Dir(value), 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(value, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return store.NewJSONLinesDiagnostics(f)
}

// realClock is the production clock.
type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

// now returns the service clock time in UTC.
func (s *Service) now() time.Time {
	return s.clock.Now().UTC()
}

// normalize applies the single set of defaults for actor and times, and
// returns the normalized request plus the clock's current UTC time.
func (s *Service) normalize(req missis.RequestContext) (missis.RequestContext, time.Time) {
	now := s.now()
	if req.Actor == "" {
		req.Actor = "human/local"
	}
	if req.EffectiveAt.IsZero() {
		req.EffectiveAt = now
	}
	if req.KnownAt.IsZero() {
		req.KnownAt = req.EffectiveAt
	}
	return req, now
}

func (s *Service) Close() error {
	return s.store.Close()
}

func (s *Service) Store() *store.Store {
	return s.store
}

func (s *Service) StorePath() string {
	return s.path
}

func (s *Service) SuppliedPath() string {
	return s.supplied
}

func (s *Service) DiscoverySource() missis.DiscoverySource {
	return s.source
}

func (s *Service) StoreID() (string, error) {
	return s.store.StoreID()
}

func (s *Service) HeadHash() (string, error) {
	return s.store.HeadHash()
}

func (s *Service) EventCount() (int64, error) {
	return s.store.EventCount()
}

func (s *Service) SchemaVersion() (string, error) {
	return s.store.SchemaVersion()
}

func (s *Service) CheckConsistency(ctx context.Context) error {
	return s.store.CheckConsistency()
}

func (s *Service) Backup(ctx context.Context, dst string) error {
	return s.store.Backup(dst)
}

func (s *Service) SequenceGaps(ctx context.Context) ([]store.SequenceGap, error) {
	return s.store.SequenceGaps()
}

func (s *Service) RepairSequenceGaps(ctx context.Context) error {
	return s.store.RepairSequenceGaps()
}

func (s *Service) RebuildProjection(ctx context.Context) error {
	return s.store.RebuildProjection()
}

func (s *Service) LoadEvents(ctx context.Context) ([]model.Event, error) {
	return s.store.LoadEvents()
}

func (s *Service) LoadLinkEvents(ctx context.Context) ([]model.Event, error) {
	return s.store.LoadLinkEvents()
}

func (s *Service) LoadTicketEvents(ctx context.Context, ticketID model.TicketID) ([]model.Event, error) {
	return s.store.LoadTicketEvents(ticketID)
}

func (s *Service) LoadStreamEvents(ctx context.Context, stream model.Ref) ([]model.Event, error) {
	return s.store.LoadStreamEvents(stream)
}

func (s *Service) CurrentProjection(ctx context.Context, ticketID model.TicketID, effectiveAt time.Time) (*model.Projection, error) {
	return s.store.CurrentProjection(ticketID, effectiveAt)
}

func (s *Service) CurrentStreamProjection(ctx context.Context, stream model.Ref, effectiveAt time.Time) (*model.Projection, error) {
	return s.store.CurrentStreamProjection(stream, effectiveAt)
}

func (s *Service) BitemporalProjection(ctx context.Context, ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return s.store.BitemporalProjection(ticketID, effectiveAt, knownAt)
}

func (s *Service) BitemporalStreamProjection(ctx context.Context, stream model.Ref, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return s.store.BitemporalStreamProjection(stream, effectiveAt, knownAt)
}

func (s *Service) GetEventByAlias(ctx context.Context, alias string) (model.Event, error) {
	return s.store.GetEventByAlias(alias)
}

func (s *Service) ListTickets(ctx context.Context, effectiveAt time.Time) ([]store.TicketSummary, error) {
	return s.store.ListTickets(effectiveAt)
}

func (s *Service) AppendBatch(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []store.Precondition, result any) (store.AppendOutcome, error) {
	outcome, err := s.store.AppendBatch(events, idempotencyKey, preconditions, result)
	if errors.Is(err, store.ErrConflict) {
		return outcome, conflict(err)
	}
	return outcome, err
}

func (s *Service) AppendTicketBatch(ctx context.Context, events []model.Event, idempotencyKey string, result any) (store.AppendOutcome, uint64, error) {
	outcome, alias, err := s.store.AppendTicketBatch(events, idempotencyKey, result)
	if errors.Is(err, store.ErrConflict) {
		return outcome, alias, conflict(err)
	}
	return outcome, alias, err
}

func (s *Service) LookupTicketAlias(ctx context.Context, ticketID model.TicketID) (uint64, error) {
	return s.store.LookupTicketAlias(ticketID)
}

func (s *Service) LookupIdempotency(key string, result any) (bool, error) {
	return s.store.LookupIdempotency(key, result)
}

func (s *Service) UpdateIdempotencyResult(key string, result any) error {
	return s.store.UpdateIdempotencyResult(key, result)
}
