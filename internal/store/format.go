package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// MinReadableStoreFormatRevision is the oldest durable store contract this
	// binary can migrate without rewriting ledger events.
	MinReadableStoreFormatRevision = 1
	// CurrentStoreFormatRevision is independent of the CLI release and Git
	// revision. It changes whenever durable encoding or interpretation becomes
	// incompatible with an older writer.
	CurrentStoreFormatRevision = 2
)

var ErrIncompatibleStoreFormat = errors.New("incompatible store format")

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
// empty path is a new store. Before migration 0007, migration 0005 and older
// are implicit revision 1 while migration 0006 is implicit revision 2.
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
		return revision, nil
	}

	highest := ""
	if len(migrations) > 0 {
		highest = migrations[len(migrations)-1]
	}
	if highest >= "0006_ordered_parts.sql" {
		return CurrentStoreFormatRevision, nil
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
	if err := probe.CheckConsistencyContext(ctx); err != nil {
		return ReadOnlyInspection{}, err
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
