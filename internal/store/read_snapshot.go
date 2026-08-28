package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ravinsharma7/missis/internal/model"
)

// ReadSnapshot is one verified SQLite read transaction. Identity claims and
// entity folds performed through it necessarily describe the same ledger
// state. It never owns a writer connection.
type ReadSnapshot struct {
	tx        *sql.Tx
	db        *sql.DB
	lease     *Lease
	closeOnce sync.Once
	closeErr  error
}

// BeginVerifiedReadSnapshot starts a verified snapshot on an already-open
// store. The Store retains ownership of its database and lease.
func (s *Store) BeginVerifiedReadSnapshot(ctx context.Context) (*ReadSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	snapshot := &ReadSnapshot{tx: tx}
	if err := snapshot.verify(ctx); err != nil {
		_ = snapshot.Close()
		return nil, err
	}
	return snapshot, nil
}

// OpenVerifiedReadSnapshot opens a current-format local store without
// creating its coordination lock, configuring WAL, migrating, repairing
// projections, or initializing artifacts.
func OpenVerifiedReadSnapshot(ctx context.Context, path string) (*ReadSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("resolve peer store path: %w", err)
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("peer store path must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("peer store path must be a regular file")
	}
	lease, err := AcquireExistingSharedLeaseReadOnly(absPath)
	if err != nil {
		return nil, err
	}
	revision, err := inspectStoreFormat(absPath)
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	if revision != CurrentStoreFormatRevision {
		_ = lease.Close()
		return nil, &StoreMigrationRequiredError{Found: revision, Target: CurrentStoreFormatRevision, Path: absPath}
	}
	dsn := sqliteReadOnlySnapshotDSN(absPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	cleanup := func() {
		_ = db.Close()
		_ = lease.Close()
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 10000`); err != nil {
		cleanup()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA query_only = ON`); err != nil {
		cleanup()
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		cleanup()
		return nil, err
	}
	snapshot := &ReadSnapshot{tx: tx, db: db, lease: lease}
	if err := snapshot.verify(ctx); err != nil {
		_ = snapshot.Close()
		return nil, err
	}
	return snapshot, nil
}

func sqliteReadOnlySnapshotDSN(absPath string) string {
	uriPath := filepath.ToSlash(absPath)
	// A Windows drive path must be represented as file:///C:/... . Without
	// the leading slash SQLite parses C: as a URI authority and rejects it.
	if filepath.VolumeName(absPath) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := &url.URL{Scheme: "file", Path: uriPath}
	query := u.Query()
	query.Set("mode", "ro")
	u.RawQuery = query.Encode()
	return u.String()
}

func (s *ReadSnapshot) verify(ctx context.Context) error {
	if s == nil || s.tx == nil {
		return fmt.Errorf("read snapshot is not open")
	}
	if _, err := s.IdentityInfoContext(ctx); err != nil {
		return fmt.Errorf("store identity verification failed: %w", err)
	}
	events, err := loadEventsContext(ctx, s.tx)
	if err != nil {
		return err
	}
	if err := verifyStoredHashChain(ctx, s.tx, events); err != nil {
		return fmt.Errorf("integrity verification failed: %w", err)
	}
	if err := verifyEventColumnsMatchPayload(ctx, s.tx); err != nil {
		return fmt.Errorf("event column verification failed: %w", err)
	}
	return nil
}

func (s *ReadSnapshot) IdentityInfoContext(ctx context.Context) (IdentityInfo, error) {
	if err := ctx.Err(); err != nil {
		return IdentityInfo{}, err
	}
	var info IdentityInfo
	err := s.tx.QueryRowContext(ctx, `
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

func (s *ReadSnapshot) GenesisHashContext(ctx context.Context) (string, error) {
	var value string
	err := s.tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT h.hash FROM events e JOIN event_hashes h ON h.event_id=e.id ORDER BY e.alias_seq ASC LIMIT 1), '')`).Scan(&value)
	return value, err
}

func (s *ReadSnapshot) HeadHashContext(ctx context.Context) (string, error) {
	var value string
	err := s.tx.QueryRowContext(ctx, `SELECT head_hash FROM store_meta WHERE singleton=1`).Scan(&value)
	return value, err
}

func (s *ReadSnapshot) HeadIntegrityEpochContext(ctx context.Context) (string, error) {
	var value string
	err := s.tx.QueryRowContext(ctx, `SELECT integrity_epoch FROM store_meta WHERE singleton=1`).Scan(&value)
	return value, err
}

func (s *ReadSnapshot) GenesisIntegrityEpochContext(ctx context.Context) (string, error) {
	var value string
	err := s.tx.QueryRowContext(ctx, `
		SELECT COALESCE((
			SELECT h.integrity_epoch FROM events e
			JOIN event_hashes h ON h.event_id=e.id
			ORDER BY e.alias_seq ASC LIMIT 1
		), (SELECT integrity_epoch FROM store_meta WHERE singleton=1))
	`).Scan(&value)
	return value, err
}

func (s *ReadSnapshot) EventCountContext(ctx context.Context) (int64, error) {
	var value int64
	err := s.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&value)
	return value, err
}

func (s *ReadSnapshot) FormatRevisionContext(ctx context.Context) (int, error) {
	var value int
	err := s.tx.QueryRowContext(ctx, `SELECT format_revision FROM store_meta WHERE singleton=1`).Scan(&value)
	return value, err
}

func (s *ReadSnapshot) LatestIdentityLineageReceiptDigestContext(ctx context.Context) (string, error) {
	var value string
	err := s.tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT receipt_digest FROM store_identity_migration_receipts WHERE receipt_id LIKE 'identity-fork:%' ORDER BY migrated_at DESC LIMIT 1), '')`).Scan(&value)
	return value, err
}

func (s *ReadSnapshot) LoadStreamEventsContext(ctx context.Context, stream model.Ref) ([]model.Event, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT event_json, alias_seq FROM events WHERE stream_kind=? AND stream_entity=? ORDER BY sequence ASC`, string(stream.Kind), stream.Entity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var raw []byte
		var aliasSeq uint64
		if err := rows.Scan(&raw, &aliasSeq); err != nil {
			return nil, err
		}
		var event model.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, err
		}
		event.AliasSeq = aliasSeq
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *ReadSnapshot) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.tx != nil {
			if err := s.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				s.closeErr = err
			}
		}
		if s.db != nil {
			if err := s.db.Close(); s.closeErr == nil {
				s.closeErr = err
			}
		}
		if s.lease != nil {
			if err := s.lease.Close(); s.closeErr == nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}
