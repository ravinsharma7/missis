package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// MinMigratableStoreFormatRevision is the oldest durable store contract the
	// explicit version-targeted migration command can inspect and migrate.
	MinMigratableStoreFormatRevision = 1
	// MinReadableStoreFormatRevision is retained as a source-compatible alias
	// for inspection/reporting callers. Normal Open accepts only Current.
	MinReadableStoreFormatRevision = MinMigratableStoreFormatRevision
	// CurrentStoreFormatRevision is independent of the CLI release and Git
	// revision. It changes whenever durable encoding or interpretation becomes
	// incompatible with an older writer.
	CurrentStoreFormatRevision = 7
)

// StoreFormatCompatibility is the release-visible physical compatibility
// claim. NormalOpenFormat is the only revision accepted by normal clients;
// MigratableFromFormats names the exact revisions understood by this
// maintenance implementation; MigrationSetDigest binds the embedded SQL
// migration catalog used to make that claim.
type StoreFormatCompatibility struct {
	NormalOpenFormat      int    `json:"normal_open_format"`
	MigratableFromFormats []int  `json:"migratable_from_formats"`
	MigrationSetDigest    string `json:"migration_set_digest"`
}

// FormatCompatibility returns a fresh compatibility value so callers cannot
// mutate package state.
func FormatCompatibility() StoreFormatCompatibility {
	from := make([]int, 0, CurrentStoreFormatRevision-MinMigratableStoreFormatRevision+1)
	for revision := MinMigratableStoreFormatRevision; revision <= CurrentStoreFormatRevision; revision++ {
		from = append(from, revision)
	}
	return StoreFormatCompatibility{
		NormalOpenFormat:      CurrentStoreFormatRevision,
		MigratableFromFormats: from,
		MigrationSetDigest:    migrationSetDigest(),
	}
}

// migrationSetDigest is domain-separated and framed so filename/content
// boundaries cannot collide:
//
// SHA256("MISSIS-STORE-MIGRATION-SET" || NUL || "v1" || NUL ||
//
//	repeated(filename || NUL || decimal-byte-length || NUL || bytes || NUL))
func migrationSetDigest() string {
	h := sha256.New()
	_, _ = h.Write([]byte("MISSIS-STORE-MIGRATION-SET\x00v1\x00"))
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		panic(fmt.Sprintf("read embedded migration catalog: %v", err))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			panic(fmt.Sprintf("read embedded migration %s: %v", entry.Name(), err))
		}
		_, _ = h.Write([]byte(entry.Name()))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(fmt.Sprintf("%d", len(data))))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

var ErrIncompatibleStoreFormat = errors.New("incompatible store format")
var ErrStoreMigrationRequired = errors.New("store migration required")

type StoreMigrationRequiredError struct {
	Found  int
	Target int
	Path   string
}

func (e *StoreMigrationRequiredError) Error() string {
	return fmt.Sprintf("%v: found revision %d; run missis-tools store migrate plan --store %q --to-format %d", ErrStoreMigrationRequired, e.Found, e.Path, e.Target)
}

func (e *StoreMigrationRequiredError) Unwrap() error { return ErrStoreMigrationRequired }

// IncompatibleStoreFormatError is returned before migrations, WAL setup,
// integrity verification, or projection repair can modify an existing store.
type IncompatibleStoreFormatError struct {
	Found int
	Min   int
	Max   int
	Cause string
}

func (e *IncompatibleStoreFormatError) Error() string {
	if e.Cause != "" {
		return fmt.Sprintf("%v: %s (found revision %d; supported %d-%d)", ErrIncompatibleStoreFormat, e.Cause, e.Found, e.Min, e.Max)
	}
	return fmt.Sprintf("%v: found revision %d; supported %d-%d", ErrIncompatibleStoreFormat, e.Found, e.Min, e.Max)
}

func (e *IncompatibleStoreFormatError) Unwrap() error { return ErrIncompatibleStoreFormat }

// inspectStoreFormat performs a read-only compatibility probe. A missing or
// empty path is a new store. Migration 0005 and older are implicit revision 1,
// while stores through migration 0007 are revision 2. Migration 0008 records
// revision 3, migration 0009 plus the identity step records revision 4, and
// migration 0010 plus its explicit receipt records revision 5, and migration
// 0011 plus its explicit receipt records revision 6, and migration 0012 plus
// its explicit receipt records revision 7.
func inspectStoreFormat(path string) (int, error) {
	return inspectStoreFormatMode(path, false)
}

func inspectStoreFormatMode(path string, immutable bool) (int, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return CurrentStoreFormatRevision, nil
	}
	if err != nil {
		return 0, err
	}
	if info.Size() == 0 {
		return CurrentStoreFormatRevision, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	dsn := "file:" + filepath.ToSlash(absPath) + "?mode=ro"
	if immutable {
		dsn += "&immutable=1"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	// Normal clients share the store lease, so this read-only probe can overlap
	// the first opener's schema transaction. Wait for that transaction rather
	// than misclassifying a transient SQLite schema lock as incompatibility.
	if _, err := db.Exec(`PRAGMA busy_timeout = 10000`); err != nil {
		return 0, err
	}

	var migrationTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&migrationTable); err != nil {
		return 0, err
	}
	if migrationTable == 0 {
		return MinReadableStoreFormatRevision, nil
	}
	migrations, err := appliedMigrationVersions(db)
	if err != nil {
		return 0, err
	}
	if unknown := firstUnknownMigration(migrations); unknown != "" {
		return 0, &IncompatibleStoreFormatError{Found: CurrentStoreFormatRevision + 1, Min: MinReadableStoreFormatRevision, Max: CurrentStoreFormatRevision, Cause: "unknown schema migration " + unknown}
	}

	var formatColumn int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('store_meta') WHERE name='format_revision'`).Scan(&formatColumn); err != nil {
		return 0, err
	}
	if formatColumn > 0 {
		var revision int
		err := db.QueryRow(`SELECT format_revision FROM store_meta WHERE singleton = 1`).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return CurrentStoreFormatRevision, nil
		}
		if err != nil {
			return 0, err
		}
		if revision < MinReadableStoreFormatRevision || revision > CurrentStoreFormatRevision {
			return 0, &IncompatibleStoreFormatError{Found: revision, Min: MinReadableStoreFormatRevision, Max: CurrentStoreFormatRevision}
		}
		if revision == CurrentStoreFormatRevision {
			var info IdentityInfo
			if err := db.QueryRow(`
				SELECT i.store_id, i.identity_scheme, i.document_bytes, i.document_digest, i.artifact_namespace
				FROM store_identity_v1 i
				JOIN store_meta m ON m.singleton = i.singleton AND m.store_id = i.store_id
				WHERE i.singleton = 1
			`).Scan(&info.StoreID, &info.Scheme, &info.DocumentBytes, &info.DocumentDigest, &info.ArtifactNamespace); err != nil {
				return 0, fmt.Errorf("inspect store identity: %w", err)
			}
			if err := validateIdentityInfo(info); err != nil {
				return 0, err
			}
		}
		return revision, nil
	}

	highest := ""
	if len(migrations) > 0 {
		highest = migrations[len(migrations)-1]
	}
	if highest >= "0006_ordered_parts.sql" {
		return 2, nil
	}
	return MinReadableStoreFormatRevision, nil
}

// ReadOnlyInspection is the result of checking an existing store without
// opening a writer, configuring WAL, running migrations, or repairing data.
type ReadOnlyInspection struct {
	FormatRevision int
}

// InspectReadOnly verifies compatibility, ledger/projection consistency, and
// sequence continuity using a read-only SQLite connection.
func InspectReadOnly(ctx context.Context, path string) (ReadOnlyInspection, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ReadOnlyInspection{}, err
	}
	if info.Size() == 0 {
		return ReadOnlyInspection{}, fmt.Errorf("store is empty")
	}
	if wal, walErr := os.Stat(path + "-wal"); walErr == nil && wal.Size() > 0 {
		return ReadOnlyInspection{}, fmt.Errorf("store has an uncheckpointed WAL; stop active clients before immutable inspection")
	} else if walErr != nil && !errors.Is(walErr, os.ErrNotExist) {
		return ReadOnlyInspection{}, walErr
	}
	revision, err := inspectStoreFormatMode(path, true)
	if err != nil {
		return ReadOnlyInspection{}, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ReadOnlyInspection{}, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(absPath)+"?mode=ro&immutable=1")
	if err != nil {
		return ReadOnlyInspection{}, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `PRAGMA query_only = ON`); err != nil {
		return ReadOnlyInspection{}, err
	}
	probe := &Store{reader: db}
	if revision == CurrentStoreFormatRevision {
		if err := probe.CheckConsistencyContext(ctx); err != nil {
			return ReadOnlyInspection{}, err
		}
	} else {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return ReadOnlyInspection{}, err
		}
		if err := verifyHashesTx(tx); err != nil {
			tx.Rollback()
			return ReadOnlyInspection{}, err
		}
		if err := tx.Commit(); err != nil {
			return ReadOnlyInspection{}, err
		}
	}
	gaps, err := probe.SequenceGapsContext(ctx)
	if err != nil {
		return ReadOnlyInspection{}, err
	}
	if len(gaps) != 0 {
		return ReadOnlyInspection{}, fmt.Errorf("sequence gaps detected")
	}
	return ReadOnlyInspection{FormatRevision: revision}, nil
}

func appliedMigrationVersions(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func firstUnknownMigration(applied []string) string {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return "<unreadable embedded migration catalog>"
	}
	known := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			known[entry.Name()] = struct{}{}
		}
	}
	unknown := make([]string, 0)
	for _, version := range applied {
		if _, ok := known[version]; !ok {
			unknown = append(unknown, version)
		}
	}
	sort.Strings(unknown)
	return strings.Join(unknown, ",")
}
