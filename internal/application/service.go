package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin"
	"github.com/ravinsharma7/missis/internal/plugin/builtin"
	"github.com/ravinsharma7/missis/internal/schema"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// Service is the single application layer behind the CLI, SDK, and TUI. It
// owns store access and all New/Show/Set orchestration; the public facade in
// pkg/missis delegates to it.
type Service struct {
	store           *store.Store
	artifactLease   *store.Lease
	path            string
	supplied        string
	source          missis.DiscoverySource
	clock           missis.Clock
	kinds           *schema.Catalog
	artifacts       artifact.Store
	artifactRoot    string
	artifactWarning string
	artifactBackend string
	ingestion       *plugin.IngestionRegistry
	diagCloser      io.Closer
	closeOnce       sync.Once
	closeErr        error
}

// Open resolves the store (flag/env/marker/default) and opens the service.
func Open(storeFlag string) (*Service, error) {
	return OpenWithClock(storeFlag, realClock{})
}

// OpenWithClock opens a service with an explicit clock (tests).
func OpenWithClock(storeFlag string, clock missis.Clock) (*Service, error) {
	resolved, err := missis.ResolveStore(storeFlag)
	if err != nil {
		return nil, fmt.Errorf("resolve missis store runtime=%s: %w", runtime.GOOS, err)
	}
	return openResolved(resolved, clock, "")
}

// OpenPath opens a service at an explicit store path.
func OpenPath(path string) (*Service, error) {
	return OpenPathWithClock(path, realClock{})
}

// OpenPathWithClock opens a service at an explicit path with a test clock.
func OpenPathWithClock(path string, clock missis.Clock) (*Service, error) {
	return openResolved(missis.ResolvedStore{Path: path, Supplied: path, Source: missis.DiscoveryFlag}, clock, "")
}

// OpenPathWithClockAndArtifactRoot is used by isolated tests and deployments
// that need an explicit local artifact root without changing MISSIS_STORE.
func OpenPathWithClockAndArtifactRoot(path string, clock missis.Clock, artifactRoot string) (*Service, error) {
	return openResolved(missis.ResolvedStore{Path: path, Supplied: path, Source: missis.DiscoveryFlag}, clock, artifactRoot)
}

func openResolved(resolved missis.ResolvedStore, clock missis.Clock, artifactRootOverride string) (*Service, error) {
	var s *store.Store
	var err error
	diag, diagCloser := storeDiagnosticsFromEnv()
	if diag != nil {
		s, err = store.OpenWithDiag(resolved.Path, diag)
	} else {
		s, err = store.Open(resolved.Path)
	}
	if err != nil {
		if diagCloser != nil {
			_ = diagCloser.Close()
		}
		return nil, fmt.Errorf("open missis store path=%q discovery=%s runtime=%s: %w", resolved.Path, resolved.Source, runtime.GOOS, err)
	}
	storeID, storeIDErr := s.StoreID()
	if storeIDErr != nil {
		_ = s.Close()
		if diagCloser != nil {
			_ = diagCloser.Close()
		}
		return nil, fmt.Errorf("read store identity for artifact namespace: %w", storeIDErr)
	}
	artifactNamespace, namespaceErr := s.ArtifactNamespaceContext(context.Background())
	if namespaceErr != nil {
		_ = s.Close()
		if diagCloser != nil {
			_ = diagCloser.Close()
		}
		return nil, fmt.Errorf("read artifact namespace for store %s: %w", storeID, namespaceErr)
	}
	if artifactRootOverride == "" {
		artifactRootOverride = os.Getenv("MISSIS_ARTIFACT_STORE")
	}
	rootResolution, rootErr := resolveArtifactRoot(resolved, artifactNamespace, artifactRootOverride)
	if rootErr != nil {
		_ = s.Close()
		if diagCloser != nil {
			_ = diagCloser.Close()
		}
		return nil, fmt.Errorf("resolve artifact store root for store %s: %w", storeID, rootErr)
	}
	artifactLease, leaseErr := store.AcquireSharedLease(rootResolution.root)
	if leaseErr != nil {
		_ = s.Close()
		if diagCloser != nil {
			_ = diagCloser.Close()
		}
		return nil, fmt.Errorf("acquire shared artifact lease root=%q: %w; set MISSIS_ARTIFACT_STORE to a shorter usable path", rootResolution.root, leaseErr)
	}
	artifactStore, artifactErr := artifact.NewLocalStore(rootResolution.root)
	if artifactErr != nil {
		_ = s.Close()
		if diagCloser != nil {
			_ = diagCloser.Close()
		}
		_ = artifactLease.Close()
		return nil, fmt.Errorf("open missis artifact store root=%q: %w; set MISSIS_ARTIFACT_STORE to a shorter usable path", rootResolution.root, artifactErr)
	}
	ingestion := plugin.NewIngestionRegistry()
	if err := ingestion.Register(plugin.IngestionRegistration{
		Manifest: builtin.MarkdownManifest(),
		ID:       "markdown",
		Selector: plugin.IngestSelector{Operation: "import-markdown", MediaType: "text/markdown", TargetKind: model.KindTicket},
		Plugin:   builtin.NewMarkdownImporter(),
	}); err != nil {
		_ = s.Close()
		if diagCloser != nil {
			_ = diagCloser.Close()
		}
		_ = artifactLease.Close()
		return nil, err
	}
	if err := ingestion.Register(plugin.IngestionRegistration{
		Manifest: builtin.ArtifactManifest(),
		ID:       "attach",
		Selector: plugin.IngestSelector{Operation: "attach-artifact"},
		Plugin:   builtin.NewArtifactAttacher(),
	}); err != nil {
		_ = s.Close()
		if diagCloser != nil {
			_ = diagCloser.Close()
		}
		_ = artifactLease.Close()
		return nil, err
	}
	return &Service{
		store:           s,
		artifactLease:   artifactLease,
		path:            resolved.Path,
		supplied:        resolved.Supplied,
		source:          resolved.Source,
		clock:           clock,
		kinds:           schema.NewCatalog(),
		artifacts:       artifactStore,
		artifactRoot:    rootResolution.root,
		artifactWarning: rootResolution.warning,
		artifactBackend: "local",
		ingestion:       ingestion,
		diagCloser:      diagCloser,
	}, nil
}

// RegisterPluginKind installs a validator-backed kind during composition. It
// is intentionally not a storage mutation and must happen before writes that
// use the kind. External plugin loading will call this only after manifest and
// capability validation.
func (s *Service) RegisterPluginKind(reg plugin.KindRegistration) error {
	if s.kinds == nil {
		s.kinds = schema.NewCatalog()
	}
	return s.kinds.Register(reg)
}

// RegisterIngestionPlugin installs an ingestion implementation during
// composition. Selection remains metadata-driven; callers never switch on a
// plugin ID to decide behavior.
func (s *Service) RegisterIngestionPlugin(reg plugin.IngestionRegistration) error {
	if s.ingestion == nil {
		s.ingestion = plugin.NewIngestionRegistry()
	}
	return s.ingestion.Register(reg)
}

func (s *Service) ArtifactStore() artifact.Store {
	return s.artifacts
}

func (s *Service) ArtifactRoot() string {
	return s.artifactRoot
}

func (s *Service) ArtifactRootWarning() string {
	return s.artifactWarning
}

// RecordArtifact indexes metadata after an artifact.Store has durably written
// the bytes. The event ledger receives only references/metadata; this method
// never copies blob content into SQLite.
func (s *Service) RecordArtifact(ctx context.Context, metadata artifact.Metadata, backend string) error {
	return s.store.RecordArtifact(ctx, store.ArtifactRecord{
		Ref:        metadata.Ref.String(),
		Algorithm:  metadata.Algorithm,
		Digest:     metadata.Digest,
		MediaType:  metadata.MediaType,
		Size:       metadata.Size,
		Backend:    backend,
		RecordedAt: s.now(),
	})
}

// storeDiagnosticsFromEnv wires append-path diagnostics (ticket #65).
// MISSIS_STORE_DIAG is a path to a JSON-lines file, or one of "1"/"true"/
// "stderr" to write to stderr. Unset, "0", or "false" disables. CI sets it
// unconditionally so every run captures structured evidence; the store itself
// stays free of environment reads.
func storeDiagnosticsFromEnv() (store.Diagnostics, io.Closer) {
	value := os.Getenv("MISSIS_STORE_DIAG")
	switch value {
	case "", "0", "false":
		return nil, nil
	case "1", "true", "stderr":
		return store.NewJSONLinesDiagnostics(os.Stderr), nil
	}
	if err := os.MkdirAll(filepath.Dir(value), 0o700); err != nil {
		return nil, nil
	}
	f, err := os.OpenFile(value, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil
	}
	return store.NewJSONLinesDiagnostics(f), f
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
	s.closeOnce.Do(func() {
		storeErr := s.store.Close()
		if s.diagCloser != nil {
			diagErr := s.diagCloser.Close()
			if storeErr == nil {
				storeErr = diagErr
			}
		}
		if s.artifactLease != nil {
			leaseErr := s.artifactLease.Close()
			if storeErr == nil {
				storeErr = leaseErr
			}
		}
		s.closeErr = storeErr
	})
	return s.closeErr
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

func (s *Service) StoreIDContext(ctx context.Context) (string, error) {
	return s.store.StoreIDContext(ctx)
}

func (s *Service) HeadHash() (string, error) {
	return s.store.HeadHash()
}

func (s *Service) HeadHashContext(ctx context.Context) (string, error) {
	return s.store.HeadHashContext(ctx)
}

func (s *Service) EventCount() (int64, error) {
	return s.store.EventCount()
}

func (s *Service) EventCountContext(ctx context.Context) (int64, error) {
	return s.store.EventCountContext(ctx)
}

func (s *Service) SchemaVersion() (string, error) {
	return s.store.SchemaVersion()
}

func (s *Service) SchemaVersionContext(ctx context.Context) (string, error) {
	return s.store.SchemaVersionContext(ctx)
}

func (s *Service) FormatRevision() (int, error) {
	return s.store.FormatRevision()
}

func (s *Service) FormatRevisionContext(ctx context.Context) (int, error) {
	return s.store.FormatRevisionContext(ctx)
}

func (s *Service) CheckConsistency(ctx context.Context) error {
	return s.store.CheckConsistencyContext(ctx)
}

func (s *Service) Backup(ctx context.Context, dst string) error {
	return s.backupTo(ctx, dst)
}

func (s *Service) SequenceGaps(ctx context.Context) ([]store.SequenceGap, error) {
	return s.store.SequenceGapsContext(ctx)
}

func (s *Service) RepairSequenceGaps(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.store.RepairSequenceGaps()
}

func (s *Service) RebuildProjection(ctx context.Context) error {
	return s.store.RebuildProjectionContext(ctx)
}

func (s *Service) LoadEvents(ctx context.Context) ([]model.Event, error) {
	return s.store.LoadEventsContext(ctx)
}

func (s *Service) LoadLinkEvents(ctx context.Context) ([]model.Event, error) {
	return s.store.LoadLinkEventsContext(ctx)
}

func (s *Service) LoadTicketEvents(ctx context.Context, ticketID model.TicketID) ([]model.Event, error) {
	return s.store.LoadTicketEventsContext(ctx, ticketID)
}

func (s *Service) LoadStreamEvents(ctx context.Context, stream model.Ref) ([]model.Event, error) {
	return s.store.LoadStreamEventsContext(ctx, stream)
}

func (s *Service) CurrentProjection(ctx context.Context, ticketID model.TicketID, effectiveAt time.Time) (*model.Projection, error) {
	return s.store.CurrentProjectionContext(ctx, ticketID, effectiveAt)
}

func (s *Service) CurrentStreamProjection(ctx context.Context, stream model.Ref, effectiveAt time.Time) (*model.Projection, error) {
	return s.store.CurrentStreamProjectionContext(ctx, stream, effectiveAt)
}

func (s *Service) BitemporalProjection(ctx context.Context, ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return s.store.BitemporalProjectionContext(ctx, ticketID, effectiveAt, knownAt)
}

func (s *Service) BitemporalStreamProjection(ctx context.Context, stream model.Ref, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return s.store.BitemporalStreamProjectionContext(ctx, stream, effectiveAt, knownAt)
}

func (s *Service) GetEventByAlias(ctx context.Context, alias string) (model.Event, error) {
	return s.store.GetEventByAliasContext(ctx, alias)
}

func (s *Service) ListTickets(ctx context.Context, effectiveAt time.Time) ([]store.TicketSummary, error) {
	return s.store.ListTicketsContext(ctx, effectiveAt)
}

func (s *Service) AppendBatch(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []store.Precondition, result any) (store.AppendOutcome, error) {
	outcome, err := s.store.AppendBatchContext(ctx, events, idempotencyKey, preconditions, result)
	if errors.Is(err, store.ErrIdempotencyMismatch) {
		return outcome, idempotencyMismatch(err)
	}
	if errors.Is(err, store.ErrConflict) {
		return outcome, conflict(err)
	}
	return outcome, err
}

func (s *Service) AppendTicketBatch(ctx context.Context, events []model.Event, idempotencyKey string, result any) (store.AppendOutcome, uint64, error) {
	outcome, alias, err := s.store.AppendTicketBatchContext(ctx, events, idempotencyKey, result)
	if errors.Is(err, store.ErrIdempotencyMismatch) {
		return outcome, alias, idempotencyMismatch(err)
	}
	if errors.Is(err, store.ErrConflict) {
		return outcome, alias, conflict(err)
	}
	return outcome, alias, err
}

func (s *Service) AppendArtifactTicketBatch(ctx context.Context, events []model.Event, idempotencyKey string, result any, artifacts []store.ArtifactRecord) (store.AppendOutcome, uint64, error) {
	outcome, alias, err := s.store.AppendArtifactTicketBatchContext(ctx, events, idempotencyKey, result, artifacts)
	if errors.Is(err, store.ErrIdempotencyMismatch) {
		return outcome, alias, idempotencyMismatch(err)
	}
	if errors.Is(err, store.ErrConflict) {
		return outcome, alias, conflict(err)
	}
	return outcome, alias, err
}

func (s *Service) AppendArtifactTicketBatchWithResult(ctx context.Context, events []model.Event, idempotencyKey string, result any, resultFactory func(uint64) any, artifacts []store.ArtifactRecord) (store.AppendOutcome, uint64, error) {
	outcome, alias, err := s.store.AppendArtifactTicketBatchContextWithResult(ctx, events, idempotencyKey, result, resultFactory, artifacts)
	if errors.Is(err, store.ErrIdempotencyMismatch) {
		return outcome, alias, idempotencyMismatch(err)
	}
	if errors.Is(err, store.ErrConflict) {
		return outcome, alias, conflict(err)
	}
	return outcome, alias, err
}

func (s *Service) LookupTicketAlias(ctx context.Context, ticketID model.TicketID) (uint64, error) {
	return s.store.LookupTicketAliasContext(ctx, ticketID)
}

func (s *Service) LookupIdempotency(key string, result any) (bool, error) {
	return s.store.LookupIdempotency(key, result)
}

func (s *Service) LookupIdempotencyContext(ctx context.Context, key string, result any) (bool, error) {
	return s.store.LookupIdempotencyContext(ctx, key, result)
}

func (s *Service) UpdateIdempotencyResult(key string, result any) error {
	return s.store.UpdateIdempotencyResult(key, result)
}

func (s *Service) UpdateIdempotencyResultContext(ctx context.Context, key string, result any) error {
	return s.store.UpdateIdempotencyResultContext(ctx, key, result)
}
