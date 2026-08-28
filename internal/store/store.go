package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ravinsharma7/missis/internal/idgen"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/storeidentity"
	neutral "github.com/ravinsharma7/skunkwork/packages/eventstore"
)

var ErrConflict = errors.New("optimistic concurrency conflict")
var ErrIdempotencyMismatch = errors.New("idempotency request mismatch")

type Precondition struct {
	TargetEntity         string
	ExpectedCurrentEvent model.EventID
	Link                 *LinkPrecondition
}

// LinkPrecondition guards a link mutation: the retraction/assertion of
// (From, Relation, To) only applies if the current active assertion of that
// triple is ExpectedCurrentEvent. Set semantics apply until evidence
// semantics (ticket #66) land.
type LinkPrecondition struct {
	From                 model.Ref
	Relation             string
	To                   model.Ref
	ExpectedCurrentEvent model.EventID
}

type AppendOutcome struct {
	Replayed bool
	Events   []model.Event
}

// AcceptedRecordEncoder runs after authority fields are assigned and before
// any event row is inserted. It lets a neutral adapter persist its own exact
// versioned envelope while event_json remains a compatibility/index payload.
type AcceptedRecordEncoder func(model.Event) (recordCodec string, acceptedBytes []byte, err error)

type AcceptedRecord struct {
	EventID       model.EventID
	RecordCodec   string
	AcceptedBytes []byte
	ContentHash   string
}

// AcceptedChangeRecord is one exact accepted record in the store-wide
// authority order. Position is adapter-internal and must remain opaque to
// neutral consumers.
type AcceptedChangeRecord struct {
	Position uint64
	AcceptedRecord
}

// AcceptedChangeWindow is captured by one read-only SQLite transaction.
// HighWater is therefore stable even when another connection appends after
// the snapshot begins.
type AcceptedChangeWindow struct {
	StoreID        string
	IntegrityEpoch string
	Earliest       uint64
	HighWater      uint64
	Records        []AcceptedChangeRecord
}

type TicketSummary struct {
	Ref        string
	ID         model.TicketID
	Number     uint64
	Title      string
	Status     string
	RecordedAt time.Time
}

type Store struct {
	writer *sql.DB
	reader *sql.DB
	lease  *Lease

	// diag is an optional side-channel sink for append-path diagnostics
	// (ticket #65). When nil, diagnostics are disabled with zero overhead.
	// It must never influence store behavior.
	diag Diagnostics

	// appendCommitHook is test-only fault injection. When non-nil it runs just
	// before the append transaction commits, and its error is returned in place
	// of a commit failure. Production code never sets it.
	appendCommitHook func() error
	// appendLoadHook is test-only instrumentation: when non-nil it is called
	// with the stream kind and entity loaded for each append, proving appends
	// are stream-scoped. Production code never sets it.
	appendLoadHook func(streamKind, streamEntity string)
	// changeReadSnapshotHook is test-only synchronization. When non-nil it runs
	// after the accepted-change high-water mark is captured and before rows are
	// selected from the same read transaction.
	changeReadSnapshotHook func()
}

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Open(path string) (*Store, error) {
	lease, err := AcquireSharedLease(path)
	if err != nil {
		return nil, err
	}
	s, err := OpenWithLease(path, lease, nil)
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	return s, nil
}

// OpenWithDiag opens a store and attaches a side-channel diagnostics sink.
// Diagnostics must not affect store behavior; they are write-only evidence for
// CI and debugging (ticket #65).
func OpenWithDiag(path string, diag Diagnostics) (*Store, error) {
	lease, err := AcquireSharedLease(path)
	if err != nil {
		return nil, err
	}
	s, err := OpenWithLease(path, lease, diag)
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	return s, nil
}

// OpenWithLease opens a live store using an already-held compatible lease.
// Ownership transfers to the Store on success; callers must close the Store
// rather than closing the lease separately.
func OpenWithLease(path string, lease *Lease, diag Diagnostics) (*Store, error) {
	if lease == nil {
		return nil, fmt.Errorf("store lease is required")
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil || lease.Path() != absPath {
		return nil, fmt.Errorf("store lease does not protect path %q", path)
	}
	return open(path, diag, lease)
}

// OpenSnapshot opens an explicitly temporary SQLite snapshot without a live
// store lease. It is only for VACUUM INTO output, backup verification, and
// restore staging; application clients must use Open.
func OpenSnapshot(path string) (*Store, error) {
	return open(path, nil, nil)
}

func open(path string, diag Diagnostics, lease *Lease) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("store path is empty")
	}
	if dir := filepath.Dir(path); dir != "." {
		// Personal provenance data is private by default: the store directory
		// is owner-only. Shared stores require an explicit mode (future work).
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	_, statErr := os.Stat(path)
	isNew := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !isNew {
		return nil, statErr
	}
	revision, err := inspectStoreFormat(path)
	if err != nil {
		return nil, err
	}
	if !isNew && revision < CurrentStoreFormatRevision {
		return nil, &StoreMigrationRequiredError{Found: revision, Target: CurrentStoreFormatRevision, Path: filepath.Clean(path)}
	}
	writer, err := sql.Open("sqlite", path+"?_txlock=immediate")
	if err != nil {
		return nil, err
	}
	if err := configureDB(writer); err != nil {
		writer.Close()
		return nil, err
	}
	if err := migrate(writer); err != nil {
		writer.Close()
		return nil, err
	}
	identityDB := writer
	var identityReader *sql.DB
	if !isNew {
		// Existing-store identity and hash verification is read-only. Do not
		// run it through the writer DSN's _txlock=immediate transaction or a
		// concurrent append can make ordinary Open fail with SQLITE_BUSY.
		identityReader, err = sql.Open("sqlite", path)
		if err != nil {
			writer.Close()
			return nil, err
		}
		if _, err := identityReader.Exec(`PRAGMA busy_timeout = 10000`); err != nil {
			identityReader.Close()
			writer.Close()
			return nil, err
		}
		identityDB = identityReader
	}
	if err := ensureStoreIdentityAndHashes(identityDB); err != nil {
		if identityReader != nil {
			identityReader.Close()
		}
		writer.Close()
		return nil, err
	}
	if identityReader != nil {
		if err := identityReader.Close(); err != nil {
			writer.Close()
			return nil, err
		}
	}
	if err := ensureDerivedFresh(writer); err != nil {
		writer.Close()
		return nil, err
	}
	// The SQLite file is created with the process umask; tighten it to
	// owner-only so ticket content is not world-readable by default. This is
	// POSIX-scoped: Windows relies on user-profile ACLs and does not emulate
	// mode bits (ticket #55).
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			writer.Close()
			return nil, err
		}
	}
	reader, err := sql.Open("sqlite", path)
	if err != nil {
		writer.Close()
		return nil, err
	}
	if err := configureDB(reader); err != nil {
		writer.Close()
		reader.Close()
		return nil, err
	}
	writer.SetMaxOpenConns(1)
	reader.SetMaxOpenConns(4)
	return &Store{writer: writer, reader: reader, diag: diag, lease: lease}, nil
}

func (s *Store) emit(event string, fields map[string]any) {
	if s.diag == nil {
		return
	}
	s.diag.Emit(event, fields)
}

func (s *Store) Close() error {
	readerErr := s.reader.Close()
	writerErr := s.writer.Close()
	leaseErr := error(nil)
	if s.lease != nil {
		leaseErr = s.lease.Close()
	}
	if writerErr != nil {
		return writerErr
	}
	if readerErr != nil {
		return readerErr
	}
	return leaseErr
}

func (s *Store) StoreID() (string, error) {
	return s.StoreIDContext(context.Background())
}

func (s *Store) StoreIDContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var storeID string
	err := s.reader.QueryRowContext(ctx, `SELECT store_id FROM store_meta WHERE singleton = 1`).Scan(&storeID)
	return storeID, err
}

type IdentityInfo struct {
	StoreID           string
	Scheme            string
	DocumentBytes     []byte
	DocumentDigest    string
	ArtifactNamespace string
}

func (s *Store) IdentityInfoContext(ctx context.Context) (IdentityInfo, error) {
	if err := ctx.Err(); err != nil {
		return IdentityInfo{}, err
	}
	var info IdentityInfo
	err := s.reader.QueryRowContext(ctx, `
		SELECT store_id, identity_scheme, document_bytes, document_digest, artifact_namespace
		FROM store_identity_v1 WHERE singleton = 1
	`).Scan(&info.StoreID, &info.Scheme, &info.DocumentBytes, &info.DocumentDigest, &info.ArtifactNamespace)
	if err != nil {
		return IdentityInfo{}, err
	}
	if err := validateIdentityInfo(info); err != nil {
		return IdentityInfo{}, err
	}
	return info, nil
}

func (s *Store) ArtifactNamespaceContext(ctx context.Context) (string, error) {
	info, err := s.IdentityInfoContext(ctx)
	if err != nil {
		return "", err
	}
	return info.ArtifactNamespace, nil
}

func (s *Store) LatestIdentityMigrationReceiptDigestContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var digest string
	err := s.reader.QueryRowContext(ctx, `SELECT COALESCE((SELECT receipt_digest FROM store_identity_migration_receipts ORDER BY migrated_at DESC LIMIT 1), '')`).Scan(&digest)
	return digest, err
}

func (s *Store) LatestIdentityLineageReceiptDigestContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var digest string
	err := s.reader.QueryRowContext(ctx, `SELECT COALESCE((SELECT receipt_digest FROM store_identity_migration_receipts WHERE receipt_id LIKE 'identity-fork:%' ORDER BY migrated_at DESC LIMIT 1), '')`).Scan(&digest)
	return digest, err
}

func (s *Store) LatestFormatMigrationReceiptDigestContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var digest string
	err := s.reader.QueryRowContext(ctx, `SELECT COALESCE((SELECT receipt_digest FROM store_format_migration_receipts ORDER BY migrated_at DESC LIMIT 1), '')`).Scan(&digest)
	return digest, err
}

func (s *Store) HeadHash() (string, error) {
	return s.HeadHashContext(context.Background())
}

func (s *Store) HeadHashContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var headHash string
	err := s.reader.QueryRowContext(ctx, `SELECT head_hash FROM store_meta WHERE singleton = 1`).Scan(&headHash)
	return headHash, err
}

// HeadIntegrityEpochContext returns the integrity algorithm that produced the
// current head. It is part of the portable store fingerprint: a digest without
// its epoch is ambiguous across hash-algorithm transitions.
func (s *Store) HeadIntegrityEpochContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var epoch string
	err := s.reader.QueryRowContext(ctx, `SELECT integrity_epoch FROM store_meta WHERE singleton = 1`).Scan(&epoch)
	return epoch, err
}

// GenesisIntegrityEpochContext returns the epoch recorded for the first
// accepted event. Empty stores report the active head epoch.
func (s *Store) GenesisIntegrityEpochContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var epoch string
	err := s.reader.QueryRowContext(ctx, `
		SELECT COALESCE((
			SELECT h.integrity_epoch
			FROM events e
			JOIN event_hashes h ON h.event_id = e.id
			ORDER BY e.alias_seq ASC
			LIMIT 1
		), (SELECT integrity_epoch FROM store_meta WHERE singleton = 1))
	`).Scan(&epoch)
	return epoch, err
}

// GenesisHashContext returns the first accepted event hash. It is immutable
// for a store and lets peer resolvers distinguish a replica from a same-ID
// database containing different history.
func (s *Store) GenesisHashContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var genesisHash string
	err := s.reader.QueryRowContext(ctx, `
		SELECT COALESCE((
			SELECT h.hash
			FROM events e
			JOIN event_hashes h ON h.event_id = e.id
			ORDER BY e.alias_seq ASC
			LIMIT 1
		), '')
	`).Scan(&genesisHash)
	return genesisHash, err
}

func (s *Store) EventCount() (int64, error) {
	return s.EventCountContext(context.Background())
}

func (s *Store) EventCountContext(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var count int64
	err := s.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count)
	return count, err
}

func (s *Store) SchemaVersion() (string, error) {
	return s.SchemaVersionContext(context.Background())
}

func (s *Store) SchemaVersionContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var version string
	err := s.reader.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), '') FROM schema_migrations`).Scan(&version)
	version = strings.TrimSuffix(version, ".sql")
	return version, err
}

func (s *Store) FormatRevision() (int, error) {
	return s.FormatRevisionContext(context.Background())
}

func (s *Store) FormatRevisionContext(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var revision int
	err := s.reader.QueryRowContext(ctx, `SELECT format_revision FROM store_meta WHERE singleton = 1`).Scan(&revision)
	return revision, err
}

func (s *Store) Backup(dst string) error {
	return s.BackupContext(context.Background(), dst)
}

func (s *Store) BackupContext(ctx context.Context, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if dir := filepath.Dir(dst); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	quoted := strings.ReplaceAll(filepath.Clean(dst), "'", "''")
	_, err := s.writer.ExecContext(ctx, `VACUUM INTO '`+quoted+`'`)
	return err
}

func (s *Store) AllocateTicketAlias(ticketID model.TicketID) (uint64, error) {
	result, err := s.writer.Exec(`INSERT INTO ticket_aliases (ticket_id) VALUES (?)`, string(ticketID))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func (s *Store) LookupTicketAlias(ticketID model.TicketID) (uint64, error) {
	return s.LookupTicketAliasContext(context.Background(), ticketID)
}

func (s *Store) LookupTicketAliasContext(ctx context.Context, ticketID model.TicketID) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var number uint64
	err := s.reader.QueryRowContext(ctx, `SELECT number FROM ticket_aliases WHERE ticket_id = ?`, string(ticketID)).Scan(&number)
	return number, err
}

func (s *Store) LookupIdempotency(key string, result any) (bool, error) {
	return s.LookupIdempotencyContext(context.Background(), key, result)
}

func (s *Store) LookupIdempotencyContext(ctx context.Context, key string, result any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var resultJSON, storedRequestHash string
	err := s.reader.QueryRowContext(ctx, `SELECT result_json, request_hash FROM idempotency WHERE key = ?`, key).Scan(&resultJSON, &storedRequestHash)
	if err == sql.ErrNoRows {
		err = s.reader.QueryRowContext(ctx, `SELECT result_json FROM idempotency_key_tombstones WHERE key = ?`, key).Scan(&resultJSON)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if idempotencyRequestHashFromContext(ctx) != "" {
			return false, retiredIdempotencyKeyError(key)
		}
		if result != nil && resultJSON != "" {
			if err := json.Unmarshal([]byte(resultJSON), result); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err := requireMatchingIdempotencyRequest(key, storedRequestHash, idempotencyRequestHashFromContext(ctx)); err != nil {
		return false, err
	}
	if result != nil && resultJSON != "" {
		if err := json.Unmarshal([]byte(resultJSON), result); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Store) UpdateIdempotencyResult(key string, result any) error {
	return s.UpdateIdempotencyResultContext(context.Background(), key, result)
}

func (s *Store) UpdateIdempotencyResultContext(ctx context.Context, key string, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		return nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.writer.ExecContext(ctx, `UPDATE idempotency SET result_json = ? WHERE key = ?`, string(raw), key)
	return err
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version := entry.Name()
		var applied int
		err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&applied)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		data, err := migrationsFS.ReadFile("migrations/" + version)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(data)); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func configureDB(db *sql.DB) error {
	for _, pragma := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 10000`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			return err
		}
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		return err
	}
	// Assigning WAL mode takes a database lock even when the database is
	// already in WAL mode. Concurrent Open calls overlap active appends, so
	// avoid that unnecessary transition and its SQLITE_BUSY window.
	if !strings.EqualFold(journalMode, "wal") {
		if err := db.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`PRAGMA synchronous = NORMAL`); err != nil {
		return err
	}
	return nil
}

func computeEventHash(event model.Event, previousHash string) string {
	canonical := event
	canonical.AliasSeq = 0
	canonical.PreviousHash = ""
	canonical.Hash = ""
	canonical.Value = model.NormalizeValueDataForCanonical(canonical.Value)
	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256([]byte(previousHash + "\n" + string(data)))
	return hex.EncodeToString(sum[:])
}

func ensureStoreIdentityAndHashes(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var storeID string
	err = tx.QueryRow(`SELECT store_id FROM store_meta WHERE singleton = 1`).Scan(&storeID)
	if err == sql.ErrNoRows {
		document, err := storeidentity.NewDocumentV1()
		if err != nil {
			return err
		}
		documentBytes := document.CanonicalBytes()
		storeID = document.StoreID()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(`
			INSERT INTO store_identity_v1(
				singleton, store_id, identity_scheme, document_bytes, document_digest,
				artifact_namespace, created_at, creator_protocol, creator_contract_digest
			) VALUES (1, ?, ?, ?, ?, ?, ?, ?, NULL)
		`, storeID, storeidentity.Scheme, documentBytes, storeidentity.DocumentDigest(documentBytes), storeID, now, "eventstore-v3-alpha.4"); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO store_meta (singleton, store_id, head_hash, updated_at, format_revision, integrity_epoch) VALUES (1, ?, '', ?, ?, ?)`,
			storeID, now, CurrentStoreFormatRevision, canonicalEventIntegrityEpochV1,
		); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		var info IdentityInfo
		if err := tx.QueryRow(`
			SELECT store_id, identity_scheme, document_bytes, document_digest, artifact_namespace
			FROM store_identity_v1 WHERE singleton = 1
		`).Scan(&info.StoreID, &info.Scheme, &info.DocumentBytes, &info.DocumentDigest, &info.ArtifactNamespace); err != nil {
			return fmt.Errorf("read store identity document: %w", err)
		}
		if info.StoreID != storeID {
			return fmt.Errorf("store identity mismatch: store_meta=%q identity_document=%q", storeID, info.StoreID)
		}
		if err := validateIdentityInfo(info); err != nil {
			return err
		}
	}

	// Read the snapshot and verify the existing chain inside one transaction
	// so a concurrent append cannot commit between the read and the check.
	// Normal open never rewrites integrity metadata.
	if err := verifyHashesTx(tx); err != nil {
		return fmt.Errorf("integrity verification failed: %w", err)
	}
	if err := verifyFormatMigrationReceipts(context.Background(), tx); err != nil {
		return fmt.Errorf("format migration verification failed: %w", err)
	}
	return tx.Commit()
}

func validateIdentityInfo(info IdentityInfo) error {
	if info.Scheme != storeidentity.Scheme {
		return fmt.Errorf("unsupported store identity scheme %q", info.Scheme)
	}
	if err := storeidentity.ValidateBinding(info.StoreID, info.DocumentBytes); err != nil {
		return fmt.Errorf("invalid store identity: %w", err)
	}
	if got := storeidentity.DocumentDigest(info.DocumentBytes); got != info.DocumentDigest {
		return fmt.Errorf("store identity document digest mismatch: stored=%q computed=%q", info.DocumentDigest, got)
	}
	if strings.TrimSpace(info.ArtifactNamespace) == "" {
		return fmt.Errorf("store artifact namespace is empty")
	}
	return nil
}

func verifyHashesTx(tx *sql.Tx) error {
	events, err := loadEventsTx(tx)
	if err != nil {
		return err
	}
	return verifyStoredHashChain(context.Background(), tx, events)
}

// verifyFormatMigrationReceipts is deliberately narrower than
// CheckConsistency: strict Open calls it without refolding projections. A
// format migration receipt is part of the interpretation boundary, so Open
// must reject tampered bytes or denormalized index fields before serving any
// event.
func verifyFormatMigrationReceipts(ctx context.Context, db contextSQL) error {
	rows, err := db.QueryContext(ctx, `SELECT
		receipt_id,source_format_revision,target_format_revision,store_id,
		source_head_digest,source_head_integrity_epoch,source_event_count,
		backup_database_sha256,migrated_at,receipt_bytes,receipt_digest
		FROM store_format_migration_receipts`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var receiptID, storeID, sourceHead, sourceEpoch, backupDigest, migratedAt, digest string
		var sourceRevision, targetRevision int
		var sourceEventCount int64
		var raw []byte
		if err := rows.Scan(&receiptID, &sourceRevision, &targetRevision, &storeID,
			&sourceHead, &sourceEpoch, &sourceEventCount, &backupDigest, &migratedAt, &raw, &digest); err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		hexSum := hex.EncodeToString(sum[:])
		if receiptID != "format-migration:"+hexSum || digest != "sha256:"+hexSum {
			return fmt.Errorf("format migration receipt %q digest mismatch", receiptID)
		}
		var receipt formatMigrationReceiptV1
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return fmt.Errorf("format migration receipt %q cannot be decoded: %w", receiptID, err)
		}
		if receipt.Version != "store-format-migration-v1" || receipt.StoreID != storeID ||
			receipt.SourceHeadDigest != sourceHead || receipt.SourceHeadIntegrityEpoch != sourceEpoch ||
			receipt.SourceEventCount != sourceEventCount || receipt.SourceFormatRevision != sourceRevision ||
			receipt.TargetFormatRevision != targetRevision || receipt.BackupDatabaseSHA256 != backupDigest || receipt.MigratedAt != migratedAt {
			return fmt.Errorf("format migration receipt %q indexed fields disagree with receipt bytes", receiptID)
		}
		if targetRevision > CurrentStoreFormatRevision {
			return fmt.Errorf("format migration receipt %q targets unsupported format %d", receiptID, targetRevision)
		}
	}
	return rows.Err()
}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") ||
		strings.Contains(text, "sqlite_busy") ||
		strings.Contains(text, "database table is locked")
}

func isRetryableAppendError(err error) bool {
	if isBusyError(err) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique constraint failed: events.stream_kind, events.stream_entity, events.sequence")
}

func insertTicketAliasTxContext(ctx context.Context, tx *sql.Tx, ticketID model.TicketID) (uint64, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO ticket_aliases (ticket_id) VALUES (?)`, string(ticketID))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func aliasForTicketTxContext(ctx context.Context, tx *sql.Tx, ticketID model.TicketID) (uint64, error) {
	var number uint64
	err := tx.QueryRowContext(ctx, `SELECT number FROM ticket_aliases WHERE ticket_id = ?`, string(ticketID)).Scan(&number)
	return number, err
}

func (s *Store) CheckConsistency() error {
	return s.CheckConsistencyContext(context.Background())
}

func (s *Store) CheckConsistencyContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Run the whole check against one read snapshot so a concurrent append
	// committing between statements cannot produce a spurious mismatch.
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var identity IdentityInfo
	if err := tx.QueryRowContext(ctx, `SELECT store_id,identity_scheme,document_bytes,document_digest,artifact_namespace FROM store_identity_v1 WHERE singleton=1`).Scan(
		&identity.StoreID, &identity.Scheme, &identity.DocumentBytes, &identity.DocumentDigest, &identity.ArtifactNamespace,
	); err != nil {
		return fmt.Errorf("store identity consistency: %w", err)
	}
	if err := validateIdentityInfo(identity); err != nil {
		return err
	}
	var metaStoreID string
	if err := tx.QueryRowContext(ctx, `SELECT store_id FROM store_meta WHERE singleton=1`).Scan(&metaStoreID); err != nil {
		return err
	}
	if metaStoreID != identity.StoreID {
		return fmt.Errorf("store identity consistency: store_meta=%q document=%q", metaStoreID, identity.StoreID)
	}
	ancestorIDs := map[string]bool{identity.StoreID: true}
	lineageParents := make(map[string]string)
	identityDocumentDigests := map[string]string{identity.StoreID: identity.DocumentDigest}
	lineageChildDigests := make(map[string]string)
	lineageRows, err := tx.QueryContext(ctx, `SELECT receipt_id,from_store_id,to_store_id,receipt_bytes,receipt_digest FROM store_identity_migration_receipts WHERE receipt_id LIKE 'identity-fork:%'`)
	if err != nil {
		return err
	}
	for lineageRows.Next() {
		var receiptID, fromStoreID, toStoreID, digest string
		var raw []byte
		if err := lineageRows.Scan(&receiptID, &fromStoreID, &toStoreID, &raw, &digest); err != nil {
			lineageRows.Close()
			return err
		}
		sum := sha256.Sum256(raw)
		receiptHex := hex.EncodeToString(sum[:])
		if digest != "sha256:"+receiptHex || receiptID != "identity-fork:"+receiptHex {
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q digest mismatch", receiptID)
		}
		var header struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q cannot be decoded", receiptID)
		}
		var receipt writableForkReceiptV1
		var receiptV2 writableForkReceiptV2
		switch header.Version {
		case "store-identity-fork-v1":
			if err := json.Unmarshal(raw, &receipt); err != nil {
				lineageRows.Close()
				return fmt.Errorf("identity lineage receipt %q cannot be decoded", receiptID)
			}
		case "store-identity-fork-v2":
			if err := json.Unmarshal(raw, &receiptV2); err != nil {
				lineageRows.Close()
				return fmt.Errorf("identity lineage receipt %q cannot be decoded", receiptID)
			}
			receipt = receiptV2.writableForkReceiptV1
		default:
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q has unknown version %q", receiptID, header.Version)
		}
		if receipt.FromStoreID != fromStoreID || receipt.ToStoreID != toStoreID {
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q fields do not match indexed values", receiptID)
		}
		if err := storeidentity.ValidateBinding(fromStoreID, receipt.FromIdentityDocument); err != nil {
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q parent binding: %w", receiptID, err)
		}
		if storeidentity.DocumentDigest(receipt.FromIdentityDocument) != receipt.FromIdentityDocumentDigest {
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q parent document digest mismatch", receiptID)
		}
		if prior, exists := identityDocumentDigests[fromStoreID]; exists && prior != receipt.FromIdentityDocumentDigest {
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q conflicts with parent document digest", receiptID)
		}
		identityDocumentDigests[fromStoreID] = receipt.FromIdentityDocumentDigest
		if err := storeidentity.ValidateStoreID(toStoreID); err != nil {
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q child identity: %w", receiptID, err)
		}
		validDisposition := receipt.ArtifactDisposition == "new-empty-namespace"
		if header.Version == "store-identity-fork-v2" {
			validDisposition = receipt.ArtifactDisposition == "copied-independent-namespace-v1" &&
				receiptV2.UnmanagedDisposition == "provenance-only-unmanaged-v1" &&
				receiptV2.ExcludedObjectDisposition == "excluded-unreferenced-v1" &&
				receiptV2.ArtifactForkProtocol == "artifact-namespace-fork-v1" &&
				receiptV2.FromArtifactNamespace != "" && receiptV2.ArtifactManifestDigest != "" && receiptV2.CompletionMarkerDigest != ""
			var manifestDigest, markerDigest string
			var copiedObjects, copiedBytes, unmanagedCount, excludedCount int64
			if err := tx.QueryRowContext(ctx, `SELECT manifest_digest,completion_marker_digest,copied_object_count,copied_byte_count,unmanaged_reference_count,excluded_object_count FROM artifact_namespace_forks WHERE receipt_id=?`, receiptID).Scan(
				&manifestDigest, &markerDigest, &copiedObjects, &copiedBytes, &unmanagedCount, &excludedCount,
			); err != nil || manifestDigest != receiptV2.ArtifactManifestDigest || markerDigest != receiptV2.CompletionMarkerDigest ||
				copiedObjects != int64(receiptV2.CopiedObjectCount) || copiedBytes != receiptV2.CopiedByteCount ||
				unmanagedCount != int64(receiptV2.UnmanagedReferenceCount) || excludedCount != int64(receiptV2.ExcludedUnreferencedObjCount) {
				lineageRows.Close()
				return fmt.Errorf("identity lineage receipt %q artifact namespace index mismatch", receiptID)
			}
		}
		if receipt.ToIdentityScheme != storeidentity.Scheme || !validDisposition || receipt.ArtifactNamespace != toStoreID {
			lineageRows.Close()
			return fmt.Errorf("identity lineage receipt %q has invalid child scheme or artifact disposition", receiptID)
		}
		lineageChildDigests[toStoreID] = receipt.ToIdentityDocumentDigest
		if _, duplicate := lineageParents[toStoreID]; duplicate {
			lineageRows.Close()
			return fmt.Errorf("identity lineage has multiple parents for %q", toStoreID)
		}
		lineageParents[toStoreID] = fromStoreID
	}
	if err := lineageRows.Err(); err != nil {
		lineageRows.Close()
		return err
	}
	if err := lineageRows.Close(); err != nil {
		return err
	}
	for child := identity.StoreID; ; {
		parent, ok := lineageParents[child]
		if !ok {
			break
		}
		if ancestorIDs[parent] {
			return fmt.Errorf("identity lineage cycle reaches %q", parent)
		}
		ancestorIDs[parent] = true
		child = parent
	}
	if len(lineageParents) != len(ancestorIDs)-1 {
		return fmt.Errorf("identity lineage contains a branch disconnected from current store %q", identity.StoreID)
	}
	for storeID, childDigest := range lineageChildDigests {
		if knownDigest := identityDocumentDigests[storeID]; childDigest != knownDigest {
			return fmt.Errorf("identity lineage child %q document digest mismatch: receipt=%q known=%q", storeID, childDigest, knownDigest)
		}
	}
	receiptRows, err := tx.QueryContext(ctx, `SELECT receipt_id,to_store_id,receipt_bytes,receipt_digest FROM store_identity_migration_receipts WHERE receipt_id LIKE 'identity-migration:%'`)
	if err != nil {
		return err
	}
	for receiptRows.Next() {
		var receiptID, toStoreID, digest string
		var raw []byte
		if err := receiptRows.Scan(&receiptID, &toStoreID, &raw, &digest); err != nil {
			receiptRows.Close()
			return err
		}
		sum := sha256.Sum256(raw)
		computed := "sha256:" + hex.EncodeToString(sum[:])
		if computed != digest || receiptID != "identity-migration:"+hex.EncodeToString(sum[:]) {
			receiptRows.Close()
			return fmt.Errorf("identity migration receipt %q digest mismatch", receiptID)
		}
		if !ancestorIDs[toStoreID] {
			receiptRows.Close()
			return fmt.Errorf("identity migration receipt %q targets %q, which is not an ancestor of current store %q", receiptID, toStoreID, identity.StoreID)
		}
	}
	if err := receiptRows.Err(); err != nil {
		receiptRows.Close()
		return err
	}
	if err := receiptRows.Close(); err != nil {
		return err
	}
	formatRows, err := tx.QueryContext(ctx, `SELECT
		receipt_id,source_format_revision,target_format_revision,store_id,
		source_head_digest,source_head_integrity_epoch,source_event_count,
		backup_database_sha256,migrated_at,receipt_bytes,receipt_digest
		FROM store_format_migration_receipts`)
	if err != nil {
		return err
	}
	for formatRows.Next() {
		var receiptID, receiptStoreID, sourceHead, sourceEpoch, backupDigest, migratedAt, digest string
		var sourceRevision, targetRevision int
		var sourceEventCount int64
		var raw []byte
		if err := formatRows.Scan(&receiptID, &sourceRevision, &targetRevision, &receiptStoreID,
			&sourceHead, &sourceEpoch, &sourceEventCount, &backupDigest, &migratedAt, &raw, &digest); err != nil {
			formatRows.Close()
			return err
		}
		sum := sha256.Sum256(raw)
		hexSum := hex.EncodeToString(sum[:])
		if receiptID != "format-migration:"+hexSum || digest != "sha256:"+hexSum {
			formatRows.Close()
			return fmt.Errorf("format migration receipt %q digest mismatch", receiptID)
		}
		var receipt formatMigrationReceiptV1
		if err := json.Unmarshal(raw, &receipt); err != nil {
			formatRows.Close()
			return fmt.Errorf("format migration receipt %q cannot be decoded: %w", receiptID, err)
		}
		if receipt.Version != "store-format-migration-v1" || receipt.StoreID != receiptStoreID ||
			receipt.SourceHeadDigest != sourceHead || receipt.SourceHeadIntegrityEpoch != sourceEpoch ||
			receipt.SourceEventCount != sourceEventCount || receipt.SourceFormatRevision != sourceRevision ||
			receipt.TargetFormatRevision != targetRevision || receipt.BackupDatabaseSHA256 != backupDigest || receipt.MigratedAt != migratedAt {
			formatRows.Close()
			return fmt.Errorf("format migration receipt %q indexed fields disagree with receipt bytes", receiptID)
		}
		if !ancestorIDs[receiptStoreID] || targetRevision > CurrentStoreFormatRevision {
			formatRows.Close()
			return fmt.Errorf("format migration receipt %q does not apply to the current identity/format", receiptID)
		}
	}
	if err := formatRows.Err(); err != nil {
		formatRows.Close()
		return err
	}
	if err := formatRows.Close(); err != nil {
		return err
	}
	epochLineageRows, err := tx.QueryContext(ctx, `SELECT receipt_id,store_id FROM integrity_epoch_transition_receipts`)
	if err != nil {
		return err
	}
	for epochLineageRows.Next() {
		var receiptID, receiptStoreID string
		if err := epochLineageRows.Scan(&receiptID, &receiptStoreID); err != nil {
			epochLineageRows.Close()
			return err
		}
		if !ancestorIDs[receiptStoreID] {
			epochLineageRows.Close()
			return fmt.Errorf("integrity epoch transition receipt %q belongs to unrelated store %q", receiptID, receiptStoreID)
		}
	}
	if err := epochLineageRows.Err(); err != nil {
		epochLineageRows.Close()
		return err
	}
	if err := epochLineageRows.Close(); err != nil {
		return err
	}

	events, err := loadEventsContext(ctx, tx)
	if err != nil {
		return err
	}
	byStream := make(map[string][]model.Event)
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := string(event.Stream.Kind) + ":" + event.Stream.Entity
		byStream[key] = append(byStream[key], event)
	}
	for stream, streamEvents := range byStream {
		if err := ctx.Err(); err != nil {
			return err
		}
		// loadEventsTx returns events in acceptance (alias_seq) order, so the
		// invariant is: sequence values are unique and strictly increasing in
		// acceptance order. Gaps are allowed and reported separately by
		// SequenceGaps as integrity incidents; they are not erased.
		var previous uint64
		for i, event := range streamEvents {
			if err := ctx.Err(); err != nil {
				return err
			}
			if i > 0 && event.Sequence <= previous {
				return fmt.Errorf("stream %s sequence out of order or duplicate: got %d after %d", stream, event.Sequence, previous)
			}
			previous = event.Sequence
		}
	}
	if err := verifyDerivedVsLedger(ctx, tx, byStream); err != nil {
		return err
	}
	if err := verifyEventColumnsMatchPayload(ctx, tx); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT key, event_ids_json, request_hash FROM idempotency`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var key, eventIDsJSON, requestHash string
		if err := rows.Scan(&key, &eventIDsJSON, &requestHash); err != nil {
			return err
		}
		var ids []string
		if err := json.Unmarshal([]byte(eventIDsJSON), &ids); err != nil {
			return fmt.Errorf("idempotency %s has invalid event ids: %w", key, err)
		}
		if err := validateIdempotencyRequestHash(requestHash); err != nil {
			return fmt.Errorf("idempotency %s has invalid request hash: %w", key, err)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tombstones, err := tx.QueryContext(ctx, `SELECT key, event_ids_json, result_json, reason FROM idempotency_key_tombstones`)
	if err != nil {
		return err
	}
	for tombstones.Next() {
		if err := ctx.Err(); err != nil {
			tombstones.Close()
			return err
		}
		var key, eventIDsJSON, resultJSON, reason string
		if err := tombstones.Scan(&key, &eventIDsJSON, &resultJSON, &reason); err != nil {
			tombstones.Close()
			return err
		}
		var ids []string
		if err := json.Unmarshal([]byte(eventIDsJSON), &ids); err != nil {
			tombstones.Close()
			return fmt.Errorf("idempotency tombstone %s has invalid event ids: %w", key, err)
		}
		var result any
		if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
			tombstones.Close()
			return fmt.Errorf("idempotency tombstone %s has invalid result: %w", key, err)
		}
		if reason != "format-v2-unbound-request" {
			tombstones.Close()
			return fmt.Errorf("idempotency tombstone %s has unknown reason %q", key, reason)
		}
	}
	if err := tombstones.Err(); err != nil {
		tombstones.Close()
		return err
	}
	if err := tombstones.Close(); err != nil {
		return err
	}
	var hashCount int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_hashes`).Scan(&hashCount); err != nil {
		return err
	}
	if hashCount != int64(len(events)) {
		return fmt.Errorf("event hash count mismatch: got %d, want %d", hashCount, len(events))
	}
	if err := verifyStoredHashChain(ctx, tx, events); err != nil {
		return err
	}
	return nil
}

// verifyStoredHashChain recomputes the chain from the event rows and compares
// every stored (previous_hash, hash) row plus the final head hash. A mismatch
// means either the event bytes or the integrity metadata changed outside the
// append path.
type contextSQL interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifyStoredHashChain(ctx context.Context, tx contextSQL, events []model.Event) error {
	var canonicalSchemaColumns int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('events') WHERE name IN ('record_codec','accepted_bytes','content_hash')`).Scan(&canonicalSchemaColumns); err != nil {
		return err
	}
	if canonicalSchemaColumns != 3 {
		return verifyGlobalJSONHashChain(ctx, tx, events)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT h.previous_hash,h.hash,h.integrity_epoch,
		       e.record_codec,e.accepted_bytes,e.content_hash,e.alias_seq
		FROM event_hashes h
		JOIN events e ON e.id = h.event_id
		ORDER BY e.alias_seq ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var authorityStoreID string
	if err := tx.QueryRowContext(ctx, `SELECT store_id FROM store_meta WHERE singleton=1`).Scan(&authorityStoreID); err != nil {
		return err
	}
	previous := ""
	activeObservedEpoch := ""
	globalEventCount := int64(0)
	var lastGlobalAlias uint64
	var transition *integrityEpochObservation
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return fmt.Errorf("event hash rows shorter than event ledger")
		}
		var storedPrevious, storedHash, integrityEpoch string
		var recordCodec, contentHash sql.NullString
		var acceptedBytes []byte
		var aliasSeq uint64
		if err := rows.Scan(&storedPrevious, &storedHash, &integrityEpoch, &recordCodec, &acceptedBytes, &contentHash, &aliasSeq); err != nil {
			return err
		}
		var expected string
		switch integrityEpoch {
		case globalJSONIntegrityEpochV1:
			if activeObservedEpoch == canonicalEventIntegrityEpochV1 {
				return fmt.Errorf("integrity epoch regressed to %q at event %s", integrityEpoch, event.ID)
			}
			if recordCodec.Valid || acceptedBytes != nil || contentHash.Valid {
				return fmt.Errorf("historical event %s fabricates canonical accepted-byte metadata", event.ID)
			}
			expected = computeEventHash(event, previous)
			activeObservedEpoch = globalJSONIntegrityEpochV1
			globalEventCount++
			lastGlobalAlias = aliasSeq
		case canonicalEventIntegrityEpochV1:
			if !recordCodec.Valid {
				return fmt.Errorf("canonical event %s is missing its record codec", event.ID)
			}
			if recordCodec.String != canonicalEventRecordCodecV1 && recordCodec.String != neutralEventRecordCodecV1 {
				return fmt.Errorf("event %s uses unsupported record codec %q; exact bytes preserved", event.ID, recordCodec.String)
			}
			if acceptedBytes == nil || !contentHash.Valid {
				return fmt.Errorf("canonical event %s is missing exact accepted bytes or content digest", event.ID)
			}
			computedContentHash := model.EventContentHashV1(acceptedBytes)
			if contentHash.String != computedContentHash {
				return fmt.Errorf("canonical event %s content digest mismatch: stored=%q computed=%q", event.ID, contentHash.String, computedContentHash)
			}
			expected = model.ComputeEventHashBytesV1(acceptedBytes, previous)
			switch recordCodec.String {
			case canonicalEventRecordCodecV1:
				var decoded model.Event
				if err := json.Unmarshal(acceptedBytes, &decoded); err != nil {
					return fmt.Errorf("canonical event %s cannot be decoded with %s: %w", event.ID, recordCodec.String, err)
				}
				reencoded, err := model.CanonicalEventBytesV1(decoded)
				if err != nil {
					return fmt.Errorf("canonical event %s cannot be validated: %w", event.ID, err)
				}
				if !bytes.Equal(reencoded, acceptedBytes) {
					return fmt.Errorf("canonical event %s bytes are not canonical for codec %s", event.ID, recordCodec.String)
				}
				eventBytes, err := model.CanonicalEventBytesV1(event)
				if err != nil {
					return err
				}
				if !bytes.Equal(eventBytes, acceptedBytes) {
					return fmt.Errorf("canonical event %s compatibility payload disagrees with exact accepted bytes", event.ID)
				}
			case neutralEventRecordCodecV1:
				decoded, err := neutral.DecodeAcceptedEventV1(acceptedBytes)
				if err != nil {
					return fmt.Errorf("canonical event %s cannot be decoded with %s: %w", event.ID, recordCodec.String, err)
				}
				reencoded, err := neutral.CanonicalAcceptedEventBytesV1(decoded)
				if err != nil || !bytes.Equal(reencoded, acceptedBytes) {
					return fmt.Errorf("canonical event %s bytes are not canonical for codec %s", event.ID, recordCodec.String)
				}
				if decoded.Namespace != authorityStoreID {
					return fmt.Errorf("canonical event %s neutral namespace %q does not match authority %q", event.ID, decoded.Namespace, authorityStoreID)
				}
				if err := verifyNeutralCompatibilityEvent(decoded, event); err != nil {
					return fmt.Errorf("canonical event %s compatibility indexes disagree with exact neutral envelope", event.ID)
				}
			}
			if activeObservedEpoch != canonicalEventIntegrityEpochV1 {
				transition = &integrityEpochObservation{
					SourceHead: previous, SourceEventCount: globalEventCount,
					ActivationAfterAliasSeq: lastGlobalAlias, FirstEventID: string(event.ID),
					RecordCodec:      recordCodec.String,
					FirstContentHash: contentHash.String, FirstHead: expected,
				}
			}
			activeObservedEpoch = canonicalEventIntegrityEpochV1
		default:
			return fmt.Errorf("event %s uses unsupported integrity epoch %q", event.ID, integrityEpoch)
		}
		if storedPrevious != previous || storedHash != expected {
			return fmt.Errorf("integrity mismatch at event %s: stored chain disagrees with recomputed chain", event.ID)
		}
		previous = expected
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var storedHead, activeStoredEpoch string
	if err := tx.QueryRowContext(ctx, `SELECT head_hash,integrity_epoch FROM store_meta WHERE singleton = 1`).Scan(&storedHead, &activeStoredEpoch); err != nil {
		return err
	}
	if previous != storedHead {
		return fmt.Errorf("head hash mismatch")
	}
	if activeObservedEpoch == "" {
		if activeStoredEpoch != globalJSONIntegrityEpochV1 && activeStoredEpoch != canonicalEventIntegrityEpochV1 {
			return fmt.Errorf("empty store has unsupported integrity epoch %q", activeStoredEpoch)
		}
	} else if activeObservedEpoch != activeStoredEpoch {
		return fmt.Errorf("active integrity epoch mismatch: stored=%q observed=%q", activeStoredEpoch, activeObservedEpoch)
	}
	return verifyIntegrityEpochTransitionReceipt(ctx, tx, transition)
}

func verifyNeutralCompatibilityEvent(accepted neutral.Event, compatibility model.Event) error {
	payload, ok := compatibility.Value.Data.(string)
	if !ok || compatibility.ID != model.EventID(accepted.ID) ||
		!neutralRefMatches(compatibility.Stream, accepted.Stream) ||
		compatibility.Sequence != accepted.StreamRevision || !batchIDMatches(compatibility.BatchID, accepted.BatchID) || compatibility.Operation != model.OpObserveEffect ||
		compatibility.Target.Kind != model.KindPart || compatibility.Target.Entity != "neutral-event:"+accepted.ID || len(compatibility.Target.Path) != 0 ||
		compatibility.Value.Kind != model.ValueKindJSON || compatibility.Value.Text != accepted.Type || payload != string(accepted.Payload) ||
		len(compatibility.Value.List) != 0 || compatibility.Value.Ref != nil || compatibility.Value.Retracted || compatibility.Value.OrderKey != "" ||
		!compatibility.RecordedAt.Equal(accepted.RecordedAt) || !compatibility.EffectiveAt.Equal(accepted.EffectiveAt) ||
		compatibility.Actor.Kind != accepted.Actor.Kind || compatibility.Actor.ID != accepted.Actor.ID || compatibility.Actor.Name != "" ||
		len(compatibility.Inputs) != 1 || !neutralRefMatches(compatibility.Inputs[0], accepted.Subject) ||
		len(compatibility.Sources) != 0 || len(compatibility.Causes) != 0 || len(compatibility.Effects) != 0 ||
		len(compatibility.Supersedes) != 0 || compatibility.Reason != "" || len(compatibility.Ontologies) != 0 || compatibility.Invocation != nil {
		return errors.New("neutral compatibility event does not derive from exact envelope")
	}
	return nil
}

func batchIDMatches(got *model.BatchID, want string) bool {
	if want == "" {
		return got == nil
	}
	return got != nil && string(*got) == want
}

func projectionEventFromAcceptedBytes(codec string, acceptedBytes []byte, proposed model.Event, authorityStoreID string) (model.Event, error) {
	switch codec {
	case canonicalEventRecordCodecV1:
		var decoded model.Event
		if err := json.Unmarshal(acceptedBytes, &decoded); err != nil {
			return model.Event{}, fmt.Errorf("decode %s: %w", codec, err)
		}
		reencoded, err := model.CanonicalEventBytesV1(decoded)
		if err != nil || !bytes.Equal(reencoded, acceptedBytes) {
			return model.Event{}, fmt.Errorf("bytes are not canonical for codec %s", codec)
		}
		if decoded.ID != proposed.ID || !sameModelRef(decoded.Stream, proposed.Stream) || decoded.Sequence != proposed.Sequence ||
			!decoded.RecordedAt.Equal(proposed.RecordedAt) || !decoded.EffectiveAt.Equal(proposed.EffectiveAt) {
			return model.Event{}, errors.New("accepted authority fields differ from assigned proposal")
		}
		return decoded, nil
	case neutralEventRecordCodecV1:
		decoded, err := neutral.DecodeAcceptedEventV1(acceptedBytes)
		if err != nil {
			return model.Event{}, fmt.Errorf("decode %s: %w", codec, err)
		}
		if decoded.Namespace != authorityStoreID {
			return model.Event{}, fmt.Errorf("neutral namespace %q does not match authority %q", decoded.Namespace, authorityStoreID)
		}
		if err := verifyNeutralCompatibilityEvent(decoded, proposed); err != nil {
			return model.Event{}, err
		}
		return neutralCompatibilityEvent(decoded), nil
	default:
		return model.Event{}, fmt.Errorf("unsupported accepted record codec %q", codec)
	}
}

func sameModelRef(left, right model.Ref) bool {
	if left.Kind != right.Kind || left.Entity != right.Entity || len(left.Path) != len(right.Path) {
		return false
	}
	for index := range left.Path {
		if left.Path[index] != right.Path[index] {
			return false
		}
	}
	return true
}

func neutralCompatibilityEvent(event neutral.Event) model.Event {
	result := model.Event{
		ID: model.EventID(event.ID), Stream: model.Ref{Kind: model.Kind(event.Stream.Kind), Entity: event.Stream.ID}, Sequence: event.StreamRevision,
		Operation: model.OpObserveEffect, Target: model.Ref{Kind: model.KindPart, Entity: "neutral-event:" + event.ID},
		Value:      model.Value{Kind: model.ValueKindJSON, Text: event.Type, Data: string(event.Payload)},
		RecordedAt: event.RecordedAt, EffectiveAt: event.EffectiveAt, Actor: model.ActorRef{Kind: event.Actor.Kind, ID: event.Actor.ID},
		Inputs: []model.Ref{{Kind: model.Kind(event.Subject.Kind), Entity: event.Subject.ID}},
	}
	if event.BatchID != "" {
		batchID := model.BatchID(event.BatchID)
		result.BatchID = &batchID
	}
	return result
}

func neutralRefMatches(got model.Ref, want neutral.Ref) bool {
	return got.Kind == model.Kind(want.Kind) && got.Entity == want.ID && len(got.Path) == 0
}

func verifyGlobalJSONHashChain(ctx context.Context, tx contextSQL, events []model.Event) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT h.previous_hash, h.hash
		FROM event_hashes h
		JOIN events e ON e.id = h.event_id
		ORDER BY e.alias_seq ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	previous := ""
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return fmt.Errorf("event hash rows shorter than event ledger")
		}
		var storedPrevious, storedHash string
		if err := rows.Scan(&storedPrevious, &storedHash); err != nil {
			return err
		}
		expected := computeEventHash(event, previous)
		if storedPrevious != previous || storedHash != expected {
			return fmt.Errorf("integrity mismatch at event %s: stored chain disagrees with recomputed chain", event.ID)
		}
		previous = expected
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var storedHead string
	if err := tx.QueryRowContext(ctx, `SELECT head_hash FROM store_meta WHERE singleton = 1`).Scan(&storedHead); err != nil {
		return err
	}
	if previous != storedHead {
		return fmt.Errorf("head hash mismatch")
	}
	return nil
}

// verifyEventColumnsMatchPayload ensures the denormalized query columns agree
// with the authoritative event_json payload. The JSON payload is the single
// source of truth; columns are indexes only, and any disagreement is an
// integrity failure.
func verifyEventColumnsMatchPayload(ctx context.Context, tx contextSQL) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			id,
			stream_kind,
			stream_entity,
			sequence,
			CAST(json_extract(event_json, '$.ID') AS TEXT),
			CAST(json_extract(event_json, '$.Stream.Kind') AS TEXT),
			CAST(json_extract(event_json, '$.Stream.Entity') AS TEXT),
			CAST(json_extract(event_json, '$.Sequence') AS INTEGER)
		FROM events`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var id, kind, entity, jsonID, jsonKind, jsonEntity string
		var sequence, jsonSequence int64
		if err := rows.Scan(&id, &kind, &entity, &sequence, &jsonID, &jsonKind, &jsonEntity, &jsonSequence); err != nil {
			return err
		}
		if id != jsonID || kind != jsonKind || entity != jsonEntity || sequence != jsonSequence {
			return fmt.Errorf("event %s: column/payload mismatch (columns id=%s kind=%s entity=%s sequence=%d; payload id=%s kind=%s entity=%s sequence=%d)",
				id, id, kind, entity, sequence, jsonID, jsonKind, jsonEntity, jsonSequence)
		}
	}
	return rows.Err()
}

type streamKey struct {
	kind   string
	entity string
}

type SequenceGap struct {
	StreamKind   string
	StreamEntity string
	Missing      []uint64
}

func (s *Store) SequenceGaps() ([]SequenceGap, error) {
	return s.SequenceGapsContext(context.Background())
}

func (s *Store) SequenceGapsContext(ctx context.Context) ([]SequenceGap, error) {
	events, err := s.LoadEventsContext(ctx)
	if err != nil {
		return nil, err
	}
	byStream := make(map[streamKey][]model.Event)
	for _, event := range events {
		key := streamKey{kind: string(event.Stream.Kind), entity: event.Stream.Entity}
		byStream[key] = append(byStream[key], event)
	}
	streams := make([]streamKey, 0, len(byStream))
	for key := range byStream {
		streams = append(streams, key)
	}
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].kind != streams[j].kind {
			return streams[i].kind < streams[j].kind
		}
		return streams[i].entity < streams[j].entity
	})
	var gaps []SequenceGap
	for _, key := range streams {
		streamEvents := byStream[key]
		sort.Slice(streamEvents, func(i, j int) bool {
			return streamEvents[i].Sequence < streamEvents[j].Sequence
		})
		var missing []uint64
		expected := uint64(1)
		for _, event := range streamEvents {
			for expected < event.Sequence {
				missing = append(missing, expected)
				expected++
			}
			expected = event.Sequence + 1
		}
		if len(missing) > 0 {
			gaps = append(gaps, SequenceGap{
				StreamKind:   key.kind,
				StreamEntity: key.entity,
				Missing:      missing,
			})
		}
	}
	return gaps, nil
}

func (s *Store) RepairSequenceGaps() error {
	// Intentionally refuses to rewrite accepted events. In-place renumbering
	// would change event_json and sequence bytes, invalidate the hash chain
	// and any external reference, and erase the evidence that data was lost.
	// Recovery is restore-from-backup; new-store-with-receipt (Strategy B)
	// is a deferred, opt-in tool.
	return fmt.Errorf("in-place sequence repair is disabled: accepted events are immutable; restore from a backup or create a new store with a repair receipt")
}

func (s *Store) AppendBatch(events []model.Event, idempotencyKey string, preconditions []Precondition, result any) (AppendOutcome, error) {
	outcome, _, err := s.appendBatchWithRetry(context.Background(), events, idempotencyKey, preconditions, result, false, nil)
	return outcome, err
}

func (s *Store) AppendBatchContext(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []Precondition, result any) (AppendOutcome, error) {
	if err := ctx.Err(); err != nil {
		return AppendOutcome{}, err
	}
	outcome, _, err := s.appendBatchWithRetry(ctx, events, idempotencyKey, preconditions, result, false, nil)
	return outcome, err
}

// AppendEncodedBatchContext is the temporary extraction boundary for neutral
// consumer codecs. The encoder cannot choose identity, sequence, or time; it
// receives the event only after those authority fields are final.
func (s *Store) AppendEncodedBatchContext(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []Precondition, result any, encoder AcceptedRecordEncoder) (AppendOutcome, error) {
	if encoder == nil {
		return AppendOutcome{}, fmt.Errorf("accepted record encoder is required")
	}
	if err := ctx.Err(); err != nil {
		return AppendOutcome{}, err
	}
	outcome, _, err := s.appendBatchWithRetryFactory(ctx, events, idempotencyKey, preconditions, result, false, nil, nil, encoder)
	return outcome, err
}

// AppendArtifactBatchContext commits artifact metadata and proposed events in
// one SQLite transaction. Blob bytes must already be durable in the artifact
// backend; a failed proposal therefore cannot leave ledger/index rows behind.
func (s *Store) AppendArtifactBatchContext(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []Precondition, result any, artifacts []ArtifactRecord) (AppendOutcome, error) {
	if err := ctx.Err(); err != nil {
		return AppendOutcome{}, err
	}
	outcome, _, err := s.appendBatchWithRetry(ctx, events, idempotencyKey, preconditions, result, false, artifacts)
	return outcome, err
}

// AppendArtifactTicketBatchContext commits artifact metadata and a ticket
// event batch in one SQLite transaction. The ticket alias is allocated inside
// the same transaction so concurrent clients cannot observe a ticket without
// its imported Parts or artifact index entry.
func (s *Store) AppendArtifactTicketBatchContext(ctx context.Context, events []model.Event, idempotencyKey string, result any, artifacts []ArtifactRecord) (AppendOutcome, uint64, error) {
	return s.AppendArtifactTicketBatchContextWithResult(ctx, events, idempotencyKey, result, nil, artifacts)
}

// AppendArtifactTicketBatchContextWithResult is the alias-aware form used by
// workflows whose persisted result contains the ticket's allocated #alias.
// The result factory runs inside the transaction after alias allocation and
// before the idempotency record is written, so concurrent retries receive the
// same complete result rather than a partially populated one.
func (s *Store) AppendArtifactTicketBatchContextWithResult(ctx context.Context, events []model.Event, idempotencyKey string, result any, resultFactory func(uint64) any, artifacts []ArtifactRecord) (AppendOutcome, uint64, error) {
	if err := ctx.Err(); err != nil {
		return AppendOutcome{}, 0, err
	}
	return s.appendBatchWithRetryFactory(ctx, events, idempotencyKey, nil, result, true, artifacts, resultFactory, nil)
}

func (s *Store) AppendTicketBatch(events []model.Event, idempotencyKey string, result any) (AppendOutcome, uint64, error) {
	return s.appendBatchWithRetry(context.Background(), events, idempotencyKey, nil, result, true, nil)
}

func (s *Store) AppendTicketBatchContext(ctx context.Context, events []model.Event, idempotencyKey string, result any) (AppendOutcome, uint64, error) {
	if err := ctx.Err(); err != nil {
		return AppendOutcome{}, 0, err
	}
	return s.appendBatchWithRetry(ctx, events, idempotencyKey, nil, result, true, nil)
}

func (s *Store) appendBatchWithRetry(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []Precondition, result any, allocateAlias bool, artifacts []ArtifactRecord) (AppendOutcome, uint64, error) {
	return s.appendBatchWithRetryFactory(ctx, events, idempotencyKey, preconditions, result, allocateAlias, artifacts, nil, nil)
}

func (s *Store) appendBatchWithRetryFactory(ctx context.Context, events []model.Event, idempotencyKey string, preconditions []Precondition, result any, allocateAlias bool, artifacts []ArtifactRecord, resultFactory func(uint64) any, acceptedEncoder AcceptedRecordEncoder) (AppendOutcome, uint64, error) {
	requestHash := ""
	if idempotencyKey != "" {
		requestHash = idempotencyRequestHashFromContext(ctx)
		if requestHash == "" {
			var err error
			requestHash, err = ComputeIdempotencyRequestHashV1(struct {
				Operation     string
				Events        []model.Event
				Preconditions []Precondition
				Artifacts     []ArtifactRecord
				AllocateAlias bool
			}{
				Operation:     "append-batch",
				Events:        events,
				Preconditions: preconditions,
				Artifacts:     artifacts,
				AllocateAlias: allocateAlias,
			})
			if err != nil {
				return AppendOutcome{}, 0, err
			}
		}
		if err := validateIdempotencyRequestHash(requestHash); err != nil {
			return AppendOutcome{}, 0, err
		}
	}
	var (
		outcome AppendOutcome
		alias   uint64
		err     error
	)
	originalSequences := make([]uint64, len(events))
	for i := range events {
		originalSequences[i] = events[i].Sequence
	}
	for attempt := 0; attempt < 6; attempt++ {
		attemptEvents := make([]model.Event, len(events))
		copy(attemptEvents, events)
		for i := range attemptEvents {
			attemptEvents[i].Sequence = originalSequences[i]
		}
		outcome, alias, err = s.appendBatchOnce(ctx, attemptEvents, idempotencyKey, requestHash, preconditions, result, allocateAlias, artifacts, resultFactory, acceptedEncoder)
		if err == nil || !isRetryableAppendError(err) {
			if err != nil {
				s.emit("append-attempt", map[string]any{
					"attempt":   attempt,
					"err_kind":  classifyAppendError(err),
					"err":       err.Error(),
					"retryable": false,
				})
			}
			return outcome, alias, err
		}
		sleepMS := (attempt + 1) * 100
		s.emit("append-attempt", map[string]any{
			"attempt":   attempt,
			"err_kind":  classifyAppendError(err),
			"err":       err.Error(),
			"retryable": true,
			"sleep_ms":  sleepMS,
		})
		timer := time.NewTimer(time.Duration(sleepMS) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return outcome, alias, ctx.Err()
		case <-timer.C:
		}
	}
	return outcome, alias, err
}

func (s *Store) appendBatchOnce(ctx context.Context, events []model.Event, idempotencyKey, requestHash string, preconditions []Precondition, result any, allocateAlias bool, artifacts []ArtifactRecord, resultFactory func(uint64) any, acceptedEncoder AcceptedRecordEncoder) (AppendOutcome, uint64, error) {
	if len(events) == 0 {
		return AppendOutcome{}, 0, fmt.Errorf("event batch is empty")
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return AppendOutcome{}, 0, err
	}
	defer tx.Rollback()

	if idempotencyKey != "" {
		var resultJSON string
		var eventIDsJSON string
		var storedRequestHash string
		err := tx.QueryRowContext(ctx, `SELECT result_json, event_ids_json, request_hash FROM idempotency WHERE key = ?`, idempotencyKey).Scan(&resultJSON, &eventIDsJSON, &storedRequestHash)
		if err == nil {
			if err := requireMatchingIdempotencyRequest(idempotencyKey, storedRequestHash, requestHash); err != nil {
				return AppendOutcome{}, 0, err
			}
			if result != nil && resultJSON != "" {
				if err := json.Unmarshal([]byte(resultJSON), result); err != nil {
					return AppendOutcome{}, 0, err
				}
			}
			replayedEvents, err := eventsByIDsJSONContext(ctx, tx, eventIDsJSON)
			if err != nil {
				return AppendOutcome{}, 0, err
			}
			var replayAlias uint64
			if allocateAlias {
				replayAlias, err = aliasForTicketTxContext(ctx, tx, model.TicketID(events[0].Stream.Entity))
				if err != nil {
					return AppendOutcome{}, 0, err
				}
			}
			return AppendOutcome{Replayed: true, Events: replayedEvents}, replayAlias, nil
		}
		if err != sql.ErrNoRows {
			return AppendOutcome{}, 0, err
		}
		var retired int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM idempotency_key_tombstones WHERE key = ?`, idempotencyKey).Scan(&retired)
		if err == nil {
			return AppendOutcome{}, 0, retiredIdempotencyKeyError(idempotencyKey)
		}
		if err != sql.ErrNoRows {
			return AppendOutcome{}, 0, err
		}
	}

	// Ambiguous-commit guard: a previous attempt may have committed even
	// though an error was returned (most likely under lock contention on
	// Windows). If every event ID in this batch already exists and matches,
	// treat the append as a successful replay instead of surfacing a
	// UNIQUE events.id failure (ticket #63).
	if replayed, outcome, replayAlias, err := s.replayExistingBatchTxContext(ctx, tx, events, allocateAlias); err != nil {
		return AppendOutcome{}, 0, err
	} else if replayed {
		return outcome, replayAlias, nil
	}
	for _, record := range artifacts {
		if err := insertArtifactTxContext(ctx, tx, record); err != nil {
			return AppendOutcome{}, 0, err
		}
	}

	streams := make(map[streamKey][]model.Event, len(events))
	var order []streamKey
	for _, event := range events {
		if event.Stream.Kind == "" || event.Stream.Entity == "" {
			return AppendOutcome{}, 0, fmt.Errorf("event stream is required")
		}
		key := streamKey{kind: string(event.Stream.Kind), entity: event.Stream.Entity}
		if _, ok := streams[key]; !ok {
			order = append(order, key)
		}
		streams[key] = append(streams[key], event)
	}

	var alias uint64
	if allocateAlias {
		if len(order) != 1 || order[0].kind != string(model.KindTicket) {
			return AppendOutcome{}, 0, fmt.Errorf("ticket alias requires a single ticket stream")
		}
		alias, err = insertTicketAliasTxContext(ctx, tx, model.TicketID(order[0].entity))
		if err != nil {
			return AppendOutcome{}, 0, err
		}
	}

	existingByStream := make(map[streamKey][]model.Event, len(order))
	for _, key := range order {
		existing, err := loadStreamEventsTxContext(ctx, tx, key.kind, key.entity)
		if err != nil {
			return AppendOutcome{}, 0, err
		}
		existingByStream[key] = existing
		if s.appendLoadHook != nil {
			s.appendLoadHook(key.kind, key.entity)
		}
	}

	var allEvents []model.Event
	for _, precondition := range preconditions {
		if precondition.Link != nil {
			allEvents, err = loadAllEventsTxContext(ctx, tx)
			if err != nil {
				return AppendOutcome{}, 0, err
			}
			break
		}
	}

	if err := checkPreconditions(existingByStream, allEvents, preconditions); err != nil {
		return AppendOutcome{}, 0, err
	}

	now := time.Now().UTC()
	var authorityStoreID, activeIntegrityEpoch string
	if err := tx.QueryRowContext(ctx, `SELECT store_id,integrity_epoch FROM store_meta WHERE singleton=1`).Scan(&authorityStoreID, &activeIntegrityEpoch); err != nil {
		return AppendOutcome{}, 0, err
	}
	if activeIntegrityEpoch != globalJSONIntegrityEpochV1 && activeIntegrityEpoch != canonicalEventIntegrityEpochV1 {
		return AppendOutcome{}, 0, fmt.Errorf("unsupported active integrity epoch %q", activeIntegrityEpoch)
	}
	var sourceEventCount int64
	var activationAfterAliasSeq uint64
	if activeIntegrityEpoch == globalJSONIntegrityEpochV1 {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(alias_seq),0) FROM events`).Scan(&sourceEventCount, &activationAfterAliasSeq); err != nil {
			return AppendOutcome{}, 0, err
		}
	}
	appended := make([]model.Event, 0, len(events))
	acceptedBytesByID := make(map[model.EventID][]byte, len(events))
	contentHashByID := make(map[model.EventID]string, len(events))
	recordCodecByID := make(map[model.EventID]string, len(events))
	runningByStream := make(map[streamKey][]model.Event, len(order))
	for _, key := range order {
		group := streams[key]
		existing := existingByStream[key]
		nextSequence, err := allocateSequenceTxContext(ctx, tx, key.kind, key.entity, uint64(len(group)))
		if err != nil {
			return AppendOutcome{}, 0, err
		}
		running := append([]model.Event(nil), existing...)
		for i := range group {
			event := group[i]
			if event.ID == "" {
				event.ID = model.EventID(idgen.New("event"))
			}
			allocated := nextSequence + uint64(i)
			if event.Sequence != 0 && event.Sequence != allocated {
				return AppendOutcome{}, 0, fmt.Errorf(
					"event %d: explicit sequence %d does not match allocated sequence %d",
					i, event.Sequence, allocated,
				)
			}
			event.Sequence = allocated
			if event.RecordedAt.IsZero() {
				event.RecordedAt = now
			}
			if event.EffectiveAt.IsZero() {
				event.EffectiveAt = event.RecordedAt
			}
			if err := model.ValidateAppend(running, event); err != nil {
				return AppendOutcome{}, 0, err
			}
			recordCodec := canonicalEventRecordCodecV1
			var acceptedBytes []byte
			if acceptedEncoder == nil {
				acceptedBytes, err = model.CanonicalEventBytesV1(event)
			} else {
				recordCodec, acceptedBytes, err = acceptedEncoder(event)
			}
			if err != nil {
				return AppendOutcome{}, 0, err
			}
			if strings.TrimSpace(recordCodec) == "" || len(acceptedBytes) == 0 {
				return AppendOutcome{}, 0, fmt.Errorf("accepted record encoder returned an empty codec or payload for event %s", event.ID)
			}
			if recordCodec != canonicalEventRecordCodecV1 && recordCodec != neutralEventRecordCodecV1 {
				return AppendOutcome{}, 0, fmt.Errorf("unsupported accepted record codec %q", recordCodec)
			}
			event, err = projectionEventFromAcceptedBytes(recordCodec, acceptedBytes, event, authorityStoreID)
			if err != nil {
				return AppendOutcome{}, 0, fmt.Errorf("accepted record %s: %w", event.ID, err)
			}
			if err := model.ValidateAppend(running, event); err != nil {
				return AppendOutcome{}, 0, fmt.Errorf("accepted record %s semantics: %w", event.ID, err)
			}
			contentHash := model.EventContentHashV1(acceptedBytes)
			eventJSON, err := json.Marshal(event)
			if err != nil {
				return AppendOutcome{}, 0, err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO events (id, stream_kind, stream_entity, sequence, event_json, record_codec, accepted_bytes, content_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				event.ID, key.kind, key.entity, event.Sequence, eventJSON, recordCodec, acceptedBytes, contentHash,
			); err != nil {
				return AppendOutcome{}, 0, err
			}
			var aliasSeq uint64
			if err := tx.QueryRowContext(ctx, `SELECT alias_seq FROM events WHERE id = ?`, event.ID).Scan(&aliasSeq); err != nil {
				return AppendOutcome{}, 0, err
			}
			event.AliasSeq = aliasSeq
			acceptedBytesByID[event.ID] = acceptedBytes
			contentHashByID[event.ID] = contentHash
			recordCodecByID[event.ID] = recordCodec
			running = append(running, event)
			appended = append(appended, event)
		}
		runningByStream[key] = running
	}

	var previousHash string
	if err := tx.QueryRowContext(ctx, `SELECT head_hash FROM store_meta WHERE singleton = 1`).Scan(&previousHash); err != nil {
		return AppendOutcome{}, 0, err
	}
	for index, event := range appended {
		acceptedBytes := acceptedBytesByID[event.ID]
		hash := model.ComputeEventHashBytesV1(acceptedBytes, previousHash)
		if index == 0 && activeIntegrityEpoch == globalJSONIntegrityEpochV1 {
			if err := activateCanonicalEventEpochTx(ctx, tx, previousHash, sourceEventCount, activationAfterAliasSeq, string(event.ID), recordCodecByID[event.ID], contentHashByID[event.ID], hash, now); err != nil {
				return AppendOutcome{}, 0, err
			}
			activeIntegrityEpoch = canonicalEventIntegrityEpochV1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO event_hashes (event_id, previous_hash, hash, integrity_epoch) VALUES (?, ?, ?, ?)`,
			event.ID, previousHash, hash, canonicalEventIntegrityEpochV1,
		); err != nil {
			return AppendOutcome{}, 0, err
		}
		previousHash = hash
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE store_meta SET head_hash = ?, updated_at = ? WHERE singleton = 1`,
		previousHash, now.Format(time.RFC3339Nano),
	); err != nil {
		return AppendOutcome{}, 0, err
	}
	for _, key := range order {
		if key.kind != string(model.KindTicket) {
			continue
		}
		if err := upsertTicketDerivedTxContext(ctx, tx, model.TicketID(key.entity), runningByStream[key], alias); err != nil {
			return AppendOutcome{}, 0, err
		}
	}

	if idempotencyKey != "" {
		ids := make([]string, 0, len(appended))
		for _, event := range appended {
			ids = append(ids, string(event.ID))
		}
		idsJSON, _ := json.Marshal(ids)
		resultJSON := "null"
		persistedResult := result
		if resultFactory != nil {
			persistedResult = resultFactory(alias)
		}
		if persistedResult != nil {
			raw, err := json.Marshal(persistedResult)
			if err != nil {
				return AppendOutcome{}, 0, err
			}
			resultJSON = string(raw)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO idempotency (key, event_ids_json, result_json, created_at, request_hash) VALUES (?, ?, ?, ?, ?)`,
			idempotencyKey, string(idsJSON), resultJSON, now.Format(time.RFC3339Nano), requestHash,
		); err != nil {
			return AppendOutcome{}, 0, err
		}
	}

	if s.appendCommitHook != nil {
		if err := s.appendCommitHook(); err != nil {
			return AppendOutcome{}, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AppendOutcome{}, 0, err
	}
	return AppendOutcome{Replayed: false, Events: appended}, alias, nil
}

// replayExistingBatchTx reports whether every event in the batch already
// exists with identical content, and if so returns the stored events as a
// replay outcome. A mismatch (same ID, different content) is an error.
func (s *Store) replayExistingBatchTxContext(ctx context.Context, tx *sql.Tx, events []model.Event, allocateAlias bool) (bool, AppendOutcome, uint64, error) {
	existing := make([]model.Event, 0, len(events))
	streamEntity := ""
	if len(events) > 0 {
		streamEntity = events[0].Stream.Entity
	}
	for _, event := range events {
		var raw string
		var aliasSeq uint64
		err := tx.QueryRowContext(ctx, `SELECT event_json, alias_seq FROM events WHERE id = ?`, string(event.ID)).Scan(&raw, &aliasSeq)
		if err == sql.ErrNoRows {
			s.emit("append-replay", map[string]any{
				"decision":   "absent",
				"stream":     streamEntity,
				"event_id":   string(event.ID),
				"batch_size": len(events),
			})
			return false, AppendOutcome{}, 0, nil
		}
		if err != nil {
			return false, AppendOutcome{}, 0, err
		}
		var stored model.Event
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return false, AppendOutcome{}, 0, err
		}
		stored.AliasSeq = aliasSeq
		if !sameAppendEvent(event, stored) {
			proposedJSON, _ := json.Marshal(event)
			s.emit("append-replay", map[string]any{
				"decision":      "conflict",
				"stream":        streamEntity,
				"event_id":      string(event.ID),
				"proposed_json": string(proposedJSON),
				"stored_json":   raw,
				"batch_size":    len(events),
			})
			return false, AppendOutcome{}, 0, fmt.Errorf("event %s already exists with different content", event.ID)
		}
		existing = append(existing, stored)
	}
	var alias uint64
	if allocateAlias {
		var err error
		alias, err = aliasForTicketTxContext(ctx, tx, model.TicketID(events[0].Stream.Entity))
		if err != nil {
			return false, AppendOutcome{}, 0, err
		}
	}
	if len(events) > 0 {
		s.emit("append-replay", map[string]any{
			"decision":   "replayed",
			"stream":     streamEntity,
			"event_id":   string(events[0].ID),
			"batch_size": len(events),
		})
	}
	return true, AppendOutcome{Replayed: true, Events: existing}, alias, nil
}

func classifyAppendError(err error) string {
	if err == nil {
		return "none"
	}
	if isBusyError(err) {
		return "busy"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "unique constraint failed: events.stream_kind"):
		return "unique"
	case strings.Contains(text, "already exists with different content"):
		return "conflict"
	default:
		return "other"
	}
}

// sameAppendEvent compares the caller-supplied fields of an event to its
// stored form. Sequence and AliasSeq are allocated by the store, so they are
// excluded from the comparison.
func sameAppendEvent(proposed, stored model.Event) bool {
	return proposed.Stream.Kind == stored.Stream.Kind &&
		proposed.Stream.Entity == stored.Stream.Entity &&
		proposed.Operation == stored.Operation &&
		refsEqual(proposed.Target, stored.Target) &&
		valuesEqual(proposed.Value, stored.Value) &&
		proposed.RecordedAt.Equal(stored.RecordedAt) &&
		proposed.EffectiveAt.Equal(stored.EffectiveAt) &&
		proposed.Actor.ID == stored.Actor.ID &&
		proposed.Actor.Name == stored.Actor.Name &&
		proposed.Reason == stored.Reason
}

func refsEqual(a, b model.Ref) bool {
	if a.Kind != b.Kind || a.Entity != b.Entity || len(a.Path) != len(b.Path) {
		return false
	}
	for i := range a.Path {
		if a.Path[i] != b.Path[i] {
			return false
		}
	}
	return true
}

func valuesEqual(a, b model.Value) bool {
	if a.Kind != b.Kind || a.Text != b.Text || len(a.List) != len(b.List) {
		return false
	}
	for i := range a.List {
		if a.List[i] != b.List[i] {
			return false
		}
	}
	if (a.Ref == nil) != (b.Ref == nil) {
		return false
	}
	if a.Ref != nil && !refsEqual(*a.Ref, *b.Ref) {
		return false
	}
	return fmt.Sprint(a.Data) == fmt.Sprint(b.Data)
}

func (s *Store) LoadEvents() ([]model.Event, error) {
	return s.LoadEventsContext(context.Background())
}

func (s *Store) LoadEventsContext(ctx context.Context) ([]model.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return loadEventsContext(ctx, s.reader)
}

func (s *Store) LoadLinkEvents() ([]model.Event, error) {
	return s.LoadLinkEventsContext(context.Background())
}

func (s *Store) LoadLinkEventsContext(ctx context.Context) ([]model.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.reader.QueryContext(ctx,
		`SELECT event_json, alias_seq FROM events
		 WHERE json_extract(event_json, '$.Operation') IN ('assert-link', 'retract-link', 'join-scope', 'leave-scope')
		 ORDER BY alias_seq ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var raw string
		var aliasSeq uint64
		if err := rows.Scan(&raw, &aliasSeq); err != nil {
			return nil, err
		}
		var event model.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		event.AliasSeq = aliasSeq
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) LoadTicketEvents(ticketID model.TicketID) ([]model.Event, error) {
	return s.LoadTicketEventsContext(context.Background(), ticketID)
}

func (s *Store) LoadTicketEventsContext(ctx context.Context, ticketID model.TicketID) ([]model.Event, error) {
	return s.LoadStreamEventsContext(ctx, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)})
}

func (s *Store) LoadStreamEvents(stream model.Ref) ([]model.Event, error) {
	return s.LoadStreamEventsContext(context.Background(), stream)
}

func (s *Store) LoadStreamEventsContext(ctx context.Context, stream model.Ref) ([]model.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.reader.QueryContext(ctx,
		`SELECT event_json, alias_seq FROM events WHERE stream_kind = ? AND stream_entity = ? ORDER BY sequence ASC`,
		string(stream.Kind), stream.Entity,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		var raw string
		var aliasSeq uint64
		if err := rows.Scan(&raw, &aliasSeq); err != nil {
			return nil, err
		}
		var event model.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		event.AliasSeq = aliasSeq
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) LoadAcceptedStreamRecordsContext(ctx context.Context, stream model.Ref) ([]AcceptedRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id,record_codec,accepted_bytes,content_hash FROM events WHERE stream_kind=? AND stream_entity=? ORDER BY sequence ASC`,
		string(stream.Kind), stream.Entity,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []AcceptedRecord
	for rows.Next() {
		var record AcceptedRecord
		var codec sql.NullString
		if err := rows.Scan(&record.EventID, &codec, &record.AcceptedBytes, &record.ContentHash); err != nil {
			return nil, err
		}
		if !codec.Valid || record.AcceptedBytes == nil {
			return nil, fmt.Errorf("event %s predates exact accepted-record bytes", record.EventID)
		}
		record.RecordCodec = codec.String
		records = append(records, record)
	}
	return records, rows.Err()
}

// LoadAcceptedChangeWindowContext reads a bounded page in the store-wide
// accepted order. The physical alias_seq key is deliberately contained here;
// callers expose only authority-issued opaque cursors.
func (s *Store) LoadAcceptedChangeWindowContext(ctx context.Context, after uint64, limit uint32) (AcceptedChangeWindow, error) {
	if err := ctx.Err(); err != nil {
		return AcceptedChangeWindow{}, err
	}
	if limit == 0 {
		return AcceptedChangeWindow{}, fmt.Errorf("accepted change limit must be positive")
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AcceptedChangeWindow{}, err
	}
	defer tx.Rollback()

	var window AcceptedChangeWindow
	if err := tx.QueryRowContext(ctx, `SELECT store_id,integrity_epoch FROM store_meta WHERE singleton=1`).Scan(&window.StoreID, &window.IntegrityEpoch); err != nil {
		return AcceptedChangeWindow{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(alias_seq),0),COALESCE(MAX(alias_seq),0) FROM events`).Scan(&window.Earliest, &window.HighWater); err != nil {
		return AcceptedChangeWindow{}, err
	}
	if s.changeReadSnapshotHook != nil {
		s.changeReadSnapshotHook()
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT alias_seq,id,record_codec,accepted_bytes,content_hash
		FROM events
		WHERE alias_seq>? AND alias_seq<=?
		ORDER BY alias_seq ASC
		LIMIT ?`, after, window.HighWater, limit)
	if err != nil {
		return AcceptedChangeWindow{}, err
	}
	for rows.Next() {
		var record AcceptedChangeRecord
		var codec sql.NullString
		if err := rows.Scan(&record.Position, &record.EventID, &codec, &record.AcceptedBytes, &record.ContentHash); err != nil {
			rows.Close()
			return AcceptedChangeWindow{}, err
		}
		if codec.Valid {
			record.RecordCodec = codec.String
		}
		window.Records = append(window.Records, record)
	}
	if err := rows.Close(); err != nil {
		return AcceptedChangeWindow{}, err
	}
	if err := rows.Err(); err != nil {
		return AcceptedChangeWindow{}, err
	}
	if err := tx.Commit(); err != nil {
		return AcceptedChangeWindow{}, err
	}
	return window, nil
}

func (s *Store) CurrentProjection(ticketID model.TicketID, effectiveAt time.Time) (*model.Projection, error) {
	return s.CurrentProjectionContext(context.Background(), ticketID, effectiveAt)
}

func (s *Store) CurrentProjectionContext(ctx context.Context, ticketID model.TicketID, effectiveAt time.Time) (*model.Projection, error) {
	return s.CurrentStreamProjectionContext(ctx, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, effectiveAt)
}

func (s *Store) BitemporalProjection(ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return s.BitemporalProjectionContext(context.Background(), ticketID, effectiveAt, knownAt)
}

func (s *Store) BitemporalProjectionContext(ctx context.Context, ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return s.BitemporalStreamProjectionContext(ctx, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, effectiveAt, knownAt)
}

func (s *Store) CurrentStreamProjection(stream model.Ref, effectiveAt time.Time) (*model.Projection, error) {
	return s.CurrentStreamProjectionContext(context.Background(), stream, effectiveAt)
}

func (s *Store) CurrentStreamProjectionContext(ctx context.Context, stream model.Ref, effectiveAt time.Time) (*model.Projection, error) {
	events, err := s.LoadStreamEventsContext(ctx, stream)
	if err != nil {
		return nil, err
	}
	return model.ProjectStream(events, stream, effectiveAt, model.MaxRecordedAt(events))
}

func (s *Store) BitemporalStreamProjection(stream model.Ref, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return s.BitemporalStreamProjectionContext(context.Background(), stream, effectiveAt, knownAt)
}

func (s *Store) BitemporalStreamProjectionContext(ctx context.Context, stream model.Ref, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	events, err := s.LoadStreamEventsContext(ctx, stream)
	if err != nil {
		return nil, err
	}
	return model.ProjectStream(events, stream, effectiveAt, knownAt)
}

func (s *Store) GetEventByAlias(alias string) (model.Event, error) {
	return s.GetEventByAliasContext(context.Background(), alias)
}

func (s *Store) GetEventByAliasContext(ctx context.Context, alias string) (model.Event, error) {
	if err := ctx.Err(); err != nil {
		return model.Event{}, err
	}
	if !strings.HasPrefix(alias, "@e") {
		return model.Event{}, fmt.Errorf("invalid event alias: %s", alias)
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(alias, "@e"), 10, 64)
	if err != nil {
		return model.Event{}, fmt.Errorf("invalid event alias: %s", alias)
	}
	var raw string
	err = s.reader.QueryRowContext(ctx, `SELECT event_json FROM events WHERE alias_seq = ?`, number).Scan(&raw)
	if err != nil {
		return model.Event{}, err
	}
	var event model.Event
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return model.Event{}, err
	}
	event.AliasSeq = number
	return event, nil
}

func (s *Store) ListTickets(effectiveAt time.Time) ([]TicketSummary, error) {
	return s.ListTicketsContext(context.Background(), effectiveAt)
}

func (s *Store) ListTicketsContext(ctx context.Context, effectiveAt time.Time) ([]TicketSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if effectiveAt.IsZero() {
		effectiveAt = time.Now().UTC()
	}
	maxRecorded, err := s.MaxRecordedAtContext(ctx)
	if err != nil {
		return nil, err
	}
	if !maxRecorded.IsZero() && effectiveAt.Before(maxRecorded) {
		// Historical list: fold from the ledger because the derived tables
		// only hold the current projection.
		return s.listTicketsByFoldContext(ctx, effectiveAt)
	}
	return s.listTicketsFromDerivedContext(ctx)
}

func (s *Store) listTicketsByFoldContext(ctx context.Context, effectiveAt time.Time) ([]TicketSummary, error) {
	events, err := s.LoadEventsContext(ctx)
	if err != nil {
		return nil, err
	}
	byTicket := make(map[model.TicketID][]model.Event)
	for _, event := range events {
		if event.Stream.Kind != model.KindTicket {
			continue
		}
		id := model.TicketID(event.Stream.Entity)
		byTicket[id] = append(byTicket[id], event)
	}
	summaries := make([]TicketSummary, 0, len(byTicket))
	for ticketID, ticketEvents := range byTicket {
		proj, err := model.CurrentProjection(ticketEvents, ticketID, effectiveAt)
		if err != nil {
			return nil, err
		}
		number, err := s.LookupTicketAliasContext(ctx, ticketID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			number = 0
		}
		summary := TicketSummary{ID: ticketID, Number: number}
		if number > 0 {
			summary.Ref = "#" + strconv.FormatUint(number, 10)
		} else {
			summary.Ref = "#" + shortID(ticketID)
		}
		if partID, ok := proj.Paths["title"]; ok {
			if part := proj.Parts[partID]; part != nil && part.Value != nil {
				summary.Title = part.Value.Text
			}
		}
		if partID, ok := proj.Paths["status"]; ok {
			if part := proj.Parts[partID]; part != nil && part.Value != nil {
				summary.Status = part.Value.Text
			}
		}
		for _, event := range ticketEvents {
			if summary.RecordedAt.IsZero() || event.RecordedAt.Before(summary.RecordedAt) {
				summary.RecordedAt = event.RecordedAt
			}
		}
		summaries = append(summaries, summary)
	}
	sortTicketSummaries(summaries)
	return summaries, nil
}

func (s *Store) listTicketsFromDerivedContext(ctx context.Context) ([]TicketSummary, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT ticket_id, alias, title, status, recorded_at FROM tickets ORDER BY alias ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := make([]TicketSummary, 0, 8)
	for rows.Next() {
		var (
			ticketID   string
			alias      uint64
			title      string
			status     string
			recordedAt string
		)
		if err := rows.Scan(&ticketID, &alias, &title, &status, &recordedAt); err != nil {
			return nil, err
		}
		summary := TicketSummary{
			ID:     model.TicketID(ticketID),
			Number: alias,
			Ref:    "#" + strconv.FormatUint(alias, 10),
			Title:  title,
			Status: status,
		}
		if recordedAt != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, recordedAt); err == nil {
				summary.RecordedAt = parsed
			}
		}
		summaries = append(summaries, summary)
	}
	sortTicketSummaries(summaries)
	return summaries, rows.Err()
}

// MaxRecordedAt returns the latest recorded_at across the ledger, or the zero
// time when the ledger is empty.
func (s *Store) MaxRecordedAt() (time.Time, error) {
	return s.MaxRecordedAtContext(context.Background())
}

func (s *Store) MaxRecordedAtContext(ctx context.Context) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	var raw string
	err := s.reader.QueryRowContext(ctx, `SELECT event_json FROM events ORDER BY alias_seq DESC LIMIT 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	var event model.Event
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return time.Time{}, err
	}
	return event.RecordedAt, nil
}

func loadStreamEventsTxContext(ctx context.Context, tx *sql.Tx, streamKind, streamEntity string) ([]model.Event, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT event_json, alias_seq FROM events WHERE stream_kind = ? AND stream_entity = ? ORDER BY sequence ASC`,
		streamKind, streamEntity,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		var raw string
		var aliasSeq uint64
		if err := rows.Scan(&raw, &aliasSeq); err != nil {
			return nil, err
		}
		var event model.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		event.AliasSeq = aliasSeq
		events = append(events, event)
	}
	return events, rows.Err()
}

// loadStreamEventsTx is retained for package-local projection tests and
// recovery helpers; context-aware callers use loadStreamEventsTxContext.
func loadStreamEventsTx(tx *sql.Tx, streamKind, streamEntity string) ([]model.Event, error) {
	return loadStreamEventsTxContext(context.Background(), tx, streamKind, streamEntity)
}

// upsertTicketDerivedTxContext folds one ticket's stream and writes its
// current summary and parts into the derived tables inside the append
// transaction.
func upsertTicketDerivedTxContext(ctx context.Context, tx *sql.Tx, ticketID model.TicketID, events []model.Event, alias uint64) error {
	proj, err := model.CurrentProjection(events, ticketID, model.MaxRecordedAt(events))
	if err != nil {
		return err
	}
	title, status := "", ""
	if id, ok := proj.Paths["title"]; ok {
		if part := proj.Parts[id]; part != nil && part.Value != nil {
			title = part.Value.Text
		}
	}
	if id, ok := proj.Paths["status"]; ok {
		if part := proj.Parts[id]; part != nil && part.Value != nil {
			status = part.Value.Text
		}
	}
	if alias == 0 {
		if n, err := aliasForTicketTxContext(ctx, tx, ticketID); err == nil {
			alias = n
		} else {
			for _, event := range events {
				if event.AliasSeq > alias {
					alias = event.AliasSeq
				}
			}
		}
	}
	var recordedAt string
	headEvent := ""
	for _, event := range events {
		headEvent = string(event.ID)
		if event.Operation == model.OpCreateEntity && recordedAt == "" {
			recordedAt = event.RecordedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	if recordedAt == "" && len(events) > 0 {
		recordedAt = events[0].RecordedAt.UTC().Format(time.RFC3339Nano)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tickets (ticket_id, alias, title, status, head_event, recorded_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(ticket_id) DO UPDATE SET
		   alias = excluded.alias, title = excluded.title, status = excluded.status,
		   head_event = excluded.head_event, recorded_at = excluded.recorded_at`,
		string(ticketID), alias, title, status, headEvent, recordedAt,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM parts_current WHERE ticket_id = ?`, string(ticketID)); err != nil {
		return err
	}
	for path, partID := range proj.Paths {
		part := proj.Parts[partID]
		if part == nil {
			continue
		}
		var valueJSON any
		if part.Value != nil {
			raw, err := json.Marshal(part.Value)
			if err != nil {
				return err
			}
			valueJSON = string(raw)
		}
		var parentID any
		if part.ParentID != nil {
			parentID = string(*part.ParentID)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO parts_current (ticket_id, path, part_id, value_json, value_kind, parent_id, created_by, current_event, order_key)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(ticketID), path, string(part.ID), valueJSON, string(part.ValueKind),
			parentID, string(part.CreatedBy), string(part.CurrentFrom), part.OrderKey,
		); err != nil {
			return err
		}
	}
	return nil
}

// RebuildProjection recomputes the derived tables from the authoritative
// event ledger. It is O(ledger) and intended for recovery only.
func (s *Store) RebuildProjection() error {
	return s.RebuildProjectionContext(context.Background())
}

func (s *Store) RebuildProjectionContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := rebuildProjectionTxContext(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func rebuildProjectionTx(tx *sql.Tx) error {
	return rebuildProjectionTxContext(context.Background(), tx)
}

func rebuildProjectionTxContext(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM tickets`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM parts_current`); err != nil {
		return err
	}
	events, err := loadEventsContext(ctx, tx)
	if err != nil {
		return err
	}
	byTicket := make(map[model.TicketID][]model.Event)
	for _, event := range events {
		if event.Stream.Kind != model.KindTicket {
			continue
		}
		id := model.TicketID(event.Stream.Entity)
		byTicket[id] = append(byTicket[id], event)
	}
	for ticketID, streamEvents := range byTicket {
		if err := upsertTicketDerivedTxContext(ctx, tx, ticketID, streamEvents, 0); err != nil {
			return err
		}
	}
	return nil
}

// projectionRepairHooks is an unexported test seam for forcing append/repair
// interleavings at SQLite transaction boundaries. Production callers pass no
// hooks, and the callbacks cannot alter the repair result except by returning
// a test-controlled error.
type projectionRepairHooks struct {
	afterOrderedDrift   func() error
	beforeRebuildCommit func() error
}

// ensureDerivedFresh guarantees the derived tables match the authoritative
// event ledger on open. It rebuilds them when rows are missing or stale, which
// can happen when a binary predating migration 0004 appended events (ticket
// #51). Events remain the source of truth, so rebuilding derived data is safe.
func ensureDerivedFresh(db *sql.DB) error {
	return ensureDerivedFreshWithHooks(db, nil)
}

func ensureDerivedFreshWithHooks(db *sql.DB, hooks *projectionRepairHooks) error {
	var ticketCount, eventCount, streamCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tickets`).Scan(&ticketCount); err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		return err
	}
	if eventCount == 0 {
		return nil
	}
	if err := db.QueryRow(`SELECT COUNT(DISTINCT stream_entity) FROM events WHERE stream_kind = ?`, string(model.KindTicket)).Scan(&streamCount); err != nil {
		return err
	}
	if ticketCount == 0 || ticketCount != streamCount {
		return rebuildDerivedWithHooks(db, hooks)
	}

	// Compare each ticket's derived head_event to the last event in its
	// stream; a mismatch means a stale binary appended without maintaining
	// the derived tables.
	rows, err := db.Query(
		`SELECT e.stream_entity, e.event_json FROM events e
		 WHERE e.stream_kind = ?
		   AND e.sequence = (SELECT MAX(e2.sequence) FROM events e2
		                     WHERE e2.stream_kind = e.stream_kind
		                       AND e2.stream_entity = e.stream_entity)`,
		string(model.KindTicket),
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	heads := make(map[string]string)
	for rows.Next() {
		var entity, raw string
		if err := rows.Scan(&entity, &raw); err != nil {
			return err
		}
		var event model.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return err
		}
		heads[entity] = string(event.ID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Close explicitly before issuing more queries on db. The writer is
	// configured with a single connection, so relying on database/sql to
	// auto-close rows at EOF can unnecessarily serialize or block repair.
	if err := rows.Close(); err != nil {
		return err
	}
	if len(heads) != ticketCount {
		return rebuildDerivedWithHooks(db, hooks)
	}
	for entity, head := range heads {
		var dbHead sql.NullString
		if err := db.QueryRow(`SELECT head_event FROM tickets WHERE ticket_id = ?`, entity).Scan(&dbHead); err != nil {
			return err
		}
		if !dbHead.Valid || dbHead.String != head {
			return rebuildDerivedWithHooks(db, hooks)
		}
	}
	orderedDrift, err := orderedProjectionDrift(db)
	if err != nil {
		return err
	}
	if orderedDrift {
		if hooks != nil && hooks.afterOrderedDrift != nil {
			if err := hooks.afterOrderedDrift(); err != nil {
				return err
			}
		}
		return rebuildDerivedWithHooks(db, hooks)
	}
	return nil
}

// orderedProjectionDrift checks only ticket streams that have ever carried an
// explicit order key. Streams predating order metadata retain their
// sequence/Part-ID fallback and do not pay the projection-fold cost here.
// The check is read-snapshot based; rebuildDerived obtains the writer
// transaction afterward, so concurrent appends are serialized with the
// rebuild and cannot be lost.
func orderedProjectionDrift(db *sql.DB) (bool, error) {
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT DISTINCT stream_entity
		FROM events
		WHERE stream_kind = ?
		  AND json_extract(event_json, '$.Value.OrderKey') IS NOT NULL
		  AND json_extract(event_json, '$.Value.OrderKey') <> ''`,
		string(model.KindTicket))
	if err != nil {
		return false, err
	}
	var ticketIDs []model.TicketID
	for rows.Next() {
		var entity string
		if err := rows.Scan(&entity); err != nil {
			rows.Close()
			return false, err
		}
		ticketIDs = append(ticketIDs, model.TicketID(entity))
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, ticketID := range ticketIDs {
		streamEvents, err := loadStreamEventsTx(tx, string(model.KindTicket), string(ticketID))
		if err != nil {
			return false, err
		}
		projection, err := model.CurrentProjection(streamEvents, ticketID, model.MaxRecordedAt(streamEvents))
		if err != nil {
			return false, err
		}
		rows, err := tx.Query(`SELECT path, order_key FROM parts_current WHERE ticket_id = ?`, string(ticketID))
		if err != nil {
			return false, err
		}
		got := make(map[string]string)
		for rows.Next() {
			var path, orderKey string
			if err := rows.Scan(&path, &orderKey); err != nil {
				rows.Close()
				return false, err
			}
			got[path] = orderKey
		}
		if err := rows.Close(); err != nil {
			return false, err
		}
		if err := rows.Err(); err != nil {
			return false, err
		}
		if len(got) != len(projection.Paths) {
			return true, nil
		}
		for path, partID := range projection.Paths {
			part := projection.Parts[partID]
			if part == nil {
				return true, nil
			}
			orderKey, ok := got[path]
			if !ok || orderKey != part.OrderKey {
				return true, nil
			}
		}
	}
	return false, nil
}

func rebuildDerived(db *sql.DB) error {
	return rebuildDerivedWithHooks(db, nil)
}

func rebuildDerivedWithHooks(db *sql.DB, hooks *projectionRepairHooks) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := rebuildProjectionTx(tx); err != nil {
		return err
	}
	if hooks != nil && hooks.beforeRebuildCommit != nil {
		if err := hooks.beforeRebuildCommit(); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// verifyDerivedVsLedger compares every ticket's derived rows against a fold
// of its authoritative events.
func verifyDerivedVsLedger(ctx context.Context, tx contextSQL, byStream map[string][]model.Event) error {
	for key, streamEvents := range byStream {
		if err := ctx.Err(); err != nil {
			return err
		}
		kind, entity, ok := strings.Cut(key, ":")
		if !ok || model.Kind(kind) != model.KindTicket {
			continue
		}
		ticketID := model.TicketID(entity)
		proj, err := model.CurrentProjection(streamEvents, ticketID, model.MaxRecordedAt(streamEvents))
		if err != nil {
			return err
		}
		title, status := "", ""
		if id, ok := proj.Paths["title"]; ok {
			if part := proj.Parts[id]; part != nil && part.Value != nil {
				title = part.Value.Text
			}
		}
		if id, ok := proj.Paths["status"]; ok {
			if part := proj.Parts[id]; part != nil && part.Value != nil {
				status = part.Value.Text
			}
		}
		var dbTitle, dbStatus string
		err = tx.QueryRowContext(ctx, `SELECT title, status FROM tickets WHERE ticket_id = ?`, string(ticketID)).Scan(&dbTitle, &dbStatus)
		if err == sql.ErrNoRows {
			return fmt.Errorf("derived ticket row missing for %s", ticketID)
		}
		if err != nil {
			return err
		}
		if dbTitle != title || dbStatus != status {
			return fmt.Errorf("derived ticket mismatch for %s: title %q/%q status %q/%q", ticketID, dbTitle, title, dbStatus, status)
		}
		rows, err := tx.QueryContext(ctx, `SELECT path, value_json, parent_id, order_key FROM parts_current WHERE ticket_id = ?`, string(ticketID))
		if err != nil {
			return err
		}
		gotParts := make(map[string][3]any)
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				rows.Close()
				return err
			}
			var path string
			var valueJSON any
			var parentID any
			var orderKey string
			if err := rows.Scan(&path, &valueJSON, &parentID, &orderKey); err != nil {
				rows.Close()
				return err
			}
			gotParts[path] = [3]any{valueJSON, parentID, orderKey}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for path, partID := range proj.Paths {
			part := proj.Parts[partID]
			if part == nil {
				continue
			}
			wantValue := any(nil)
			if part.Value != nil {
				raw, err := json.Marshal(part.Value)
				if err != nil {
					return err
				}
				wantValue = string(raw)
			}
			var wantParent any
			if part.ParentID != nil {
				wantParent = string(*part.ParentID)
			}
			got, ok := gotParts[path]
			if !ok {
				return fmt.Errorf("derived part row missing for %s/%s", ticketID, path)
			}
			if got[0] != wantValue || got[1] != wantParent || got[2] != part.OrderKey {
				return fmt.Errorf("derived part mismatch for %s/%s", ticketID, path)
			}
			delete(gotParts, path)
		}
		if len(gotParts) > 0 {
			return fmt.Errorf("derived part rows exceed ledger for %s: %v", ticketID, gotParts)
		}
	}
	return nil
}

func loadEventsTx(db interface {
	Query(string, ...any) (*sql.Rows, error)
}) ([]model.Event, error) {
	rows, err := db.Query(`SELECT event_json, alias_seq FROM events ORDER BY alias_seq ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		var raw string
		var aliasSeq uint64
		if err := rows.Scan(&raw, &aliasSeq); err != nil {
			return nil, err
		}
		var event model.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		event.AliasSeq = aliasSeq
		events = append(events, event)
	}
	return events, rows.Err()
}

type contextQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadEventsContext(ctx context.Context, db contextQuerier) ([]model.Event, error) {
	rows, err := db.QueryContext(ctx, `SELECT event_json, alias_seq FROM events ORDER BY alias_seq ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var raw string
		var aliasSeq uint64
		if err := rows.Scan(&raw, &aliasSeq); err != nil {
			return nil, err
		}
		var event model.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		event.AliasSeq = aliasSeq
		events = append(events, event)
	}
	return events, rows.Err()
}

func eventsByIDsJSONContext(ctx context.Context, tx *sql.Tx, idsJSON string) ([]model.Event, error) {
	var ids []string
	if err := json.Unmarshal([]byte(idsJSON), &ids); err != nil {
		return nil, err
	}
	events := make([]model.Event, 0, len(ids))
	for _, id := range ids {
		var raw string
		var aliasSeq uint64
		err := tx.QueryRowContext(ctx, `SELECT event_json, alias_seq FROM events WHERE id = ?`, id).Scan(&raw, &aliasSeq)
		if err != nil {
			return nil, err
		}
		var event model.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		event.AliasSeq = aliasSeq
		events = append(events, event)
	}
	return events, nil
}

func allocateSequenceTxContext(ctx context.Context, tx *sql.Tx, streamKind, streamEntity string, count uint64) (uint64, error) {
	var newValue uint64
	err := tx.QueryRowContext(ctx,
		`INSERT INTO streams (stream_kind, stream_entity, next_sequence) VALUES (?, ?, ?)
		 ON CONFLICT(stream_kind, stream_entity)
		 DO UPDATE SET next_sequence = next_sequence + excluded.next_sequence
		 RETURNING next_sequence`,
		streamKind, streamEntity, count,
	).Scan(&newValue)
	if err != nil {
		return 0, err
	}
	return newValue - count + 1, nil
}

func checkPreconditions(streamEvents map[streamKey][]model.Event, allEvents []model.Event, preconditions []Precondition) error {
	for _, precondition := range preconditions {
		if precondition.Link != nil {
			if err := checkLinkPrecondition(allEvents, precondition.Link); err != nil {
				return err
			}
			continue
		}
		if precondition.TargetEntity == "" || precondition.ExpectedCurrentEvent == "" {
			continue
		}
		if len(streamEvents) != 1 {
			return fmt.Errorf("part precondition requires a single ticket stream")
		}
		for _, events := range streamEvents {
			ticketID := events[0].Stream.Entity
			proj, err := model.CurrentProjection(events, model.TicketID(ticketID), model.MaxRecordedAt(events))
			if err != nil {
				return err
			}
			partID := model.PartID(precondition.TargetEntity)
			part := proj.Parts[partID]
			if part == nil {
				if precondition.ExpectedCurrentEvent != "" {
					return ErrConflict
				}
				continue
			}
			if part.CurrentFrom != precondition.ExpectedCurrentEvent {
				return ErrConflict
			}
		}
	}
	return nil
}

func checkLinkPrecondition(allEvents []model.Event, link *LinkPrecondition) error {
	if link == nil || len(allEvents) == 0 {
		return nil
	}
	at := model.MaxRecordedAt(allEvents)
	views, err := model.LinksForRef(allEvents, link.From, at, at)
	if err != nil {
		return err
	}
	for _, view := range views {
		if view.Direction != "asserted" || view.Relation != link.Relation {
			continue
		}
		if view.To.Kind != link.To.Kind || view.To.Entity != link.To.Entity {
			continue
		}
		for _, assertion := range view.Assertions {
			if assertion.CreatedBy == link.ExpectedCurrentEvent {
				return nil
			}
		}
		return ErrConflict
	}
	if link.ExpectedCurrentEvent != "" {
		return ErrConflict
	}
	return nil
}

func loadAllEventsTxContext(ctx context.Context, tx *sql.Tx) ([]model.Event, error) {
	rows, err := tx.QueryContext(ctx, `SELECT event_json, alias_seq FROM events ORDER BY stream_kind, stream_entity, sequence`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		var raw string
		var aliasSeq uint64
		if err := rows.Scan(&raw, &aliasSeq); err != nil {
			return nil, err
		}
		var event model.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		event.AliasSeq = aliasSeq
		events = append(events, event)
	}
	return events, rows.Err()
}

func shortID(id model.TicketID) string {
	raw := strings.TrimPrefix(string(id), "ticket:")
	if len(raw) > 8 {
		return raw[:8]
	}
	return raw
}

func sortTicketSummaries(summaries []TicketSummary) {
	for i := 1; i < len(summaries); i++ {
		for j := i; j > 0 && summaries[j].RecordedAt.Before(summaries[j-1].RecordedAt); j-- {
			summaries[j], summaries[j-1] = summaries[j-1], summaries[j]
		}
	}
}
