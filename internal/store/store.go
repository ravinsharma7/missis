package store

import (
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
)

var ErrConflict = errors.New("optimistic concurrency conflict")

type Precondition struct {
	TargetEntity         string
	ExpectedCurrentEvent model.EventID
}

type AppendOutcome struct {
	Replayed bool
	Events   []model.Event
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
}

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Open(path string) (*Store, error) {
	return open(path, nil)
}

// OpenWithDiag opens a store and attaches a side-channel diagnostics sink.
// Diagnostics must not affect store behavior; they are write-only evidence for
// CI and debugging (ticket #65).
func OpenWithDiag(path string, diag Diagnostics) (*Store, error) {
	return open(path, diag)
}

func open(path string, diag Diagnostics) (*Store, error) {
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
	if err := ensureStoreIdentityAndHashes(writer); err != nil {
		writer.Close()
		return nil, err
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
	return &Store{writer: writer, reader: reader, diag: diag}, nil
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
	if writerErr != nil {
		return writerErr
	}
	return readerErr
}

func (s *Store) StoreID() (string, error) {
	var storeID string
	err := s.reader.QueryRow(`SELECT store_id FROM store_meta WHERE singleton = 1`).Scan(&storeID)
	return storeID, err
}

func (s *Store) HeadHash() (string, error) {
	var headHash string
	err := s.reader.QueryRow(`SELECT head_hash FROM store_meta WHERE singleton = 1`).Scan(&headHash)
	return headHash, err
}

func (s *Store) EventCount() (int64, error) {
	var count int64
	err := s.reader.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count)
	return count, err
}

func (s *Store) SchemaVersion() (string, error) {
	var version string
	err := s.reader.QueryRow(`SELECT COALESCE(MAX(version), '') FROM schema_migrations`).Scan(&version)
	version = strings.TrimSuffix(version, ".sql")
	return version, err
}

func (s *Store) Backup(dst string) error {
	if dir := filepath.Dir(dst); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	quoted := strings.ReplaceAll(dst, "'", "''")
	_, err := s.writer.Exec(`VACUUM INTO '` + quoted + `'`)
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
	var number uint64
	err := s.reader.QueryRow(`SELECT number FROM ticket_aliases WHERE ticket_id = ?`, string(ticketID)).Scan(&number)
	return number, err
}

func (s *Store) LookupIdempotency(key string, result any) (bool, error) {
	var resultJSON string
	err := s.reader.QueryRow(`SELECT result_json FROM idempotency WHERE key = ?`, key).Scan(&resultJSON)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
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
	if key == "" {
		return nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.writer.Exec(`UPDATE idempotency SET result_json = ? WHERE key = ?`, string(raw), key)
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
	if err := db.QueryRow(`PRAGMA journal_mode = WAL`).Scan(&journalMode); err != nil {
		return err
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
		storeID = idgen.New("store")
		if _, err := tx.Exec(
			`INSERT INTO store_meta (singleton, store_id, head_hash, updated_at) VALUES (1, ?, '', ?)`,
			storeID, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Read the snapshot and verify the existing chain inside one transaction
	// so a concurrent append cannot commit between the read and the check.
	// Normal open never rewrites integrity metadata.
	if err := verifyHashesTx(tx); err != nil {
		return fmt.Errorf("integrity verification failed: %w", err)
	}
	return tx.Commit()
}

func verifyHashesTx(tx *sql.Tx) error {
	events, err := loadEventsTx(tx)
	if err != nil {
		return err
	}
	return verifyStoredHashChain(tx, events)
}

func rebuildHashesTx(tx *sql.Tx, events []model.Event) error {
	if _, err := tx.Exec(`DELETE FROM event_hashes`); err != nil {
		return err
	}
	previous := ""
	for _, event := range events {
		hash := computeEventHash(event, previous)
		if _, err := tx.Exec(
			`INSERT INTO event_hashes (event_id, previous_hash, hash) VALUES (?, ?, ?)`,
			event.ID, previous, hash,
		); err != nil {
			return err
		}
		previous = hash
	}
	if _, err := tx.Exec(
		`UPDATE store_meta SET head_hash = ?, updated_at = ? WHERE singleton = 1`,
		previous, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	return tx.Commit()
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

func insertTicketAliasTx(tx *sql.Tx, ticketID model.TicketID) (uint64, error) {
	result, err := tx.Exec(`INSERT INTO ticket_aliases (ticket_id) VALUES (?)`, string(ticketID))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func aliasForTicketTx(tx *sql.Tx, ticketID model.TicketID) (uint64, error) {
	var number uint64
	err := tx.QueryRow(`SELECT number FROM ticket_aliases WHERE ticket_id = ?`, string(ticketID)).Scan(&number)
	return number, err
}

func (s *Store) CheckConsistency() error {
	// Run the whole check against one read snapshot so a concurrent append
	// committing between statements cannot produce a spurious mismatch.
	tx, err := s.reader.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	events, err := loadEventsTx(tx)
	if err != nil {
		return err
	}
	byStream := make(map[string][]model.Event)
	for _, event := range events {
		key := string(event.Stream.Kind) + ":" + event.Stream.Entity
		byStream[key] = append(byStream[key], event)
	}
	for stream, streamEvents := range byStream {
		// loadEventsTx returns events in acceptance (alias_seq) order, so the
		// invariant is: sequence values are unique and strictly increasing in
		// acceptance order. Gaps are allowed and reported separately by
		// SequenceGaps as integrity incidents; they are not erased.
		var previous uint64
		for i, event := range streamEvents {
			if i > 0 && event.Sequence <= previous {
				return fmt.Errorf("stream %s sequence out of order or duplicate: got %d after %d", stream, event.Sequence, previous)
			}
			previous = event.Sequence
		}
	}
	if err := verifyDerivedVsLedger(tx, byStream); err != nil {
		return err
	}
	if err := verifyEventColumnsMatchPayload(tx); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT key, event_ids_json FROM idempotency`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, eventIDsJSON string
		if err := rows.Scan(&key, &eventIDsJSON); err != nil {
			return err
		}
		var ids []string
		if err := json.Unmarshal([]byte(eventIDsJSON), &ids); err != nil {
			return fmt.Errorf("idempotency %s has invalid event ids: %w", key, err)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var hashCount int64
	if err := tx.QueryRow(`SELECT COUNT(*) FROM event_hashes`).Scan(&hashCount); err != nil {
		return err
	}
	if hashCount != int64(len(events)) {
		return fmt.Errorf("event hash count mismatch: got %d, want %d", hashCount, len(events))
	}
	if err := verifyStoredHashChain(tx, events); err != nil {
		return err
	}
	return nil
}

// verifyStoredHashChain recomputes the chain from the event rows and compares
// every stored (previous_hash, hash) row plus the final head hash. A mismatch
// means either the event bytes or the integrity metadata changed outside the
// append path.
func verifyStoredHashChain(tx interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}, events []model.Event) error {
	rows, err := tx.Query(`
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
	if err := tx.QueryRow(`SELECT head_hash FROM store_meta WHERE singleton = 1`).Scan(&storedHead); err != nil {
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
func verifyEventColumnsMatchPayload(tx interface {
	Query(string, ...any) (*sql.Rows, error)
}) error {
	rows, err := tx.Query(`
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
	events, err := s.LoadEvents()
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
	outcome, _, err := s.appendBatchWithRetry(events, idempotencyKey, preconditions, result, false)
	return outcome, err
}

func (s *Store) AppendTicketBatch(events []model.Event, idempotencyKey string, result any) (AppendOutcome, uint64, error) {
	return s.appendBatchWithRetry(events, idempotencyKey, nil, result, true)
}

func (s *Store) appendBatchWithRetry(events []model.Event, idempotencyKey string, preconditions []Precondition, result any, allocateAlias bool) (AppendOutcome, uint64, error) {
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
		outcome, alias, err = s.appendBatchOnce(attemptEvents, idempotencyKey, preconditions, result, allocateAlias)
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
		time.Sleep(time.Duration(sleepMS) * time.Millisecond)
	}
	return outcome, alias, err
}

func (s *Store) appendBatchOnce(events []model.Event, idempotencyKey string, preconditions []Precondition, result any, allocateAlias bool) (AppendOutcome, uint64, error) {
	if len(events) == 0 {
		return AppendOutcome{}, 0, fmt.Errorf("event batch is empty")
	}

	tx, err := s.writer.Begin()
	if err != nil {
		return AppendOutcome{}, 0, err
	}
	defer tx.Rollback()

	if idempotencyKey != "" {
		var resultJSON string
		var eventIDsJSON string
		err := tx.QueryRow(`SELECT result_json, event_ids_json FROM idempotency WHERE key = ?`, idempotencyKey).Scan(&resultJSON, &eventIDsJSON)
		if err == nil {
			if result != nil && resultJSON != "" {
				if err := json.Unmarshal([]byte(resultJSON), result); err != nil {
					return AppendOutcome{}, 0, err
				}
			}
			replayedEvents, err := eventsByIDsJSON(tx, eventIDsJSON)
			if err != nil {
				return AppendOutcome{}, 0, err
			}
			var replayAlias uint64
			if allocateAlias {
				replayAlias, err = aliasForTicketTx(tx, model.TicketID(events[0].Stream.Entity))
				if err != nil {
					return AppendOutcome{}, 0, err
				}
			}
			return AppendOutcome{Replayed: true, Events: replayedEvents}, replayAlias, nil
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
	if replayed, outcome, replayAlias, err := s.replayExistingBatchTx(tx, events, allocateAlias); err != nil {
		return AppendOutcome{}, 0, err
	} else if replayed {
		return outcome, replayAlias, nil
	}

	if events[0].Stream.Kind == "" || events[0].Stream.Entity == "" {
		return AppendOutcome{}, 0, fmt.Errorf("event stream is required")
	}
	streamKind := string(events[0].Stream.Kind)
	streamEntity := events[0].Stream.Entity
	for _, event := range events {
		if string(event.Stream.Kind) != streamKind || event.Stream.Entity != streamEntity {
			return AppendOutcome{}, 0, fmt.Errorf("batch contains multiple streams")
		}
	}

	var alias uint64
	if allocateAlias {
		alias, err = insertTicketAliasTx(tx, model.TicketID(streamEntity))
		if err != nil {
			return AppendOutcome{}, 0, err
		}
	}

	existing, err := loadStreamEventsTx(tx, streamKind, streamEntity)
	if err != nil {
		return AppendOutcome{}, 0, err
	}
	if s.appendLoadHook != nil {
		s.appendLoadHook(streamKind, streamEntity)
	}

	if err := checkPreconditions(existing, preconditions); err != nil {
		return AppendOutcome{}, 0, err
	}

	nextSequence, err := allocateSequenceTx(tx, streamKind, streamEntity, uint64(len(events)))
	if err != nil {
		return AppendOutcome{}, 0, err
	}

	now := time.Now().UTC()
	appended := make([]model.Event, 0, len(events))
	running := append([]model.Event(nil), existing...)

	for i := range events {
		event := events[i]
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
		eventJSON, err := json.Marshal(event)
		if err != nil {
			return AppendOutcome{}, 0, err
		}
		if _, err := tx.Exec(
			`INSERT INTO events (id, stream_kind, stream_entity, sequence, event_json) VALUES (?, ?, ?, ?, ?)`,
			event.ID, streamKind, streamEntity, event.Sequence, eventJSON,
		); err != nil {
			return AppendOutcome{}, 0, err
		}
		var aliasSeq uint64
		if err := tx.QueryRow(`SELECT alias_seq FROM events WHERE id = ?`, event.ID).Scan(&aliasSeq); err != nil {
			return AppendOutcome{}, 0, err
		}
		event.AliasSeq = aliasSeq
		running = append(running, event)
		appended = append(appended, event)
	}

	var previousHash string
	if err := tx.QueryRow(`SELECT head_hash FROM store_meta WHERE singleton = 1`).Scan(&previousHash); err != nil {
		return AppendOutcome{}, 0, err
	}
	for _, event := range appended {
		hash := computeEventHash(event, previousHash)
		if _, err := tx.Exec(
			`INSERT INTO event_hashes (event_id, previous_hash, hash) VALUES (?, ?, ?)`,
			event.ID, previousHash, hash,
		); err != nil {
			return AppendOutcome{}, 0, err
		}
		previousHash = hash
	}
	if _, err := tx.Exec(
		`UPDATE store_meta SET head_hash = ?, updated_at = ? WHERE singleton = 1`,
		previousHash, now.Format(time.RFC3339Nano),
	); err != nil {
		return AppendOutcome{}, 0, err
	}
	if streamKind == string(model.KindTicket) {
		if err := upsertTicketDerivedTx(tx, model.TicketID(streamEntity), running, alias); err != nil {
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
		if result != nil {
			raw, err := json.Marshal(result)
			if err != nil {
				return AppendOutcome{}, 0, err
			}
			resultJSON = string(raw)
		}
		if _, err := tx.Exec(
			`INSERT INTO idempotency (key, event_ids_json, result_json, created_at) VALUES (?, ?, ?, ?)`,
			idempotencyKey, string(idsJSON), resultJSON, now.Format(time.RFC3339Nano),
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
func (s *Store) replayExistingBatchTx(tx *sql.Tx, events []model.Event, allocateAlias bool) (bool, AppendOutcome, uint64, error) {
	existing := make([]model.Event, 0, len(events))
	streamEntity := ""
	if len(events) > 0 {
		streamEntity = events[0].Stream.Entity
	}
	for _, event := range events {
		var raw string
		var aliasSeq uint64
		err := tx.QueryRow(`SELECT event_json, alias_seq FROM events WHERE id = ?`, string(event.ID)).Scan(&raw, &aliasSeq)
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
		alias, err = aliasForTicketTx(tx, model.TicketID(events[0].Stream.Entity))
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
	return loadEventsTx(s.reader)
}

func (s *Store) LoadLinkEvents() ([]model.Event, error) {
	rows, err := s.reader.Query(
		`SELECT event_json, alias_seq FROM events
		 WHERE json_extract(event_json, '$.Operation') IN ('assert-link', 'retract-link')
		 ORDER BY alias_seq ASC`,
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

func (s *Store) LoadTicketEvents(ticketID model.TicketID) ([]model.Event, error) {
	return s.LoadStreamEvents(model.Ref{Kind: model.KindTicket, Entity: string(ticketID)})
}

func (s *Store) LoadStreamEvents(stream model.Ref) ([]model.Event, error) {
	rows, err := s.reader.Query(
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

func (s *Store) CurrentProjection(ticketID model.TicketID, effectiveAt time.Time) (*model.Projection, error) {
	return s.CurrentStreamProjection(model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, effectiveAt)
}

func (s *Store) BitemporalProjection(ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	return s.BitemporalStreamProjection(model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, effectiveAt, knownAt)
}

func (s *Store) CurrentStreamProjection(stream model.Ref, effectiveAt time.Time) (*model.Projection, error) {
	events, err := s.LoadStreamEvents(stream)
	if err != nil {
		return nil, err
	}
	return model.ProjectStream(events, stream, effectiveAt, model.MaxRecordedAt(events))
}

func (s *Store) BitemporalStreamProjection(stream model.Ref, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	events, err := s.LoadStreamEvents(stream)
	if err != nil {
		return nil, err
	}
	return model.ProjectStream(events, stream, effectiveAt, knownAt)
}

func (s *Store) GetEventByAlias(alias string) (model.Event, error) {
	if !strings.HasPrefix(alias, "@e") {
		return model.Event{}, fmt.Errorf("invalid event alias: %s", alias)
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(alias, "@e"), 10, 64)
	if err != nil {
		return model.Event{}, fmt.Errorf("invalid event alias: %s", alias)
	}
	var raw string
	err = s.reader.QueryRow(`SELECT event_json FROM events WHERE alias_seq = ?`, number).Scan(&raw)
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
	if effectiveAt.IsZero() {
		effectiveAt = time.Now().UTC()
	}
	maxRecorded, err := s.MaxRecordedAt()
	if err != nil {
		return nil, err
	}
	if !maxRecorded.IsZero() && effectiveAt.Before(maxRecorded) {
		// Historical list: fold from the ledger because the derived tables
		// only hold the current projection.
		return s.listTicketsByFold(effectiveAt)
	}
	return s.listTicketsFromDerived()
}

func (s *Store) listTicketsByFold(effectiveAt time.Time) ([]TicketSummary, error) {
	events, err := s.LoadEvents()
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
		number, _ := s.LookupTicketAlias(ticketID)
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

func (s *Store) listTicketsFromDerived() ([]TicketSummary, error) {
	rows, err := s.reader.Query(`SELECT ticket_id, alias, title, status, recorded_at FROM tickets ORDER BY alias ASC`)
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
	var raw string
	err := s.reader.QueryRow(`SELECT event_json FROM events ORDER BY alias_seq DESC LIMIT 1`).Scan(&raw)
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

func loadStreamEventsTx(tx *sql.Tx, streamKind, streamEntity string) ([]model.Event, error) {
	rows, err := tx.Query(
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

// upsertTicketDerivedTx folds one ticket's stream and writes its current
// summary and parts into the derived tables inside the append transaction.
func upsertTicketDerivedTx(tx *sql.Tx, ticketID model.TicketID, events []model.Event, alias uint64) error {
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
		if n, err := aliasForTicketTx(tx, ticketID); err == nil {
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
	if _, err := tx.Exec(
		`INSERT INTO tickets (ticket_id, alias, title, status, head_event, recorded_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(ticket_id) DO UPDATE SET
		   alias = excluded.alias, title = excluded.title, status = excluded.status,
		   head_event = excluded.head_event, recorded_at = excluded.recorded_at`,
		string(ticketID), alias, title, status, headEvent, recordedAt,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM parts_current WHERE ticket_id = ?`, string(ticketID)); err != nil {
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
		if _, err := tx.Exec(
			`INSERT INTO parts_current (ticket_id, path, part_id, value_json, value_kind, parent_id, created_by, current_event)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(ticketID), path, string(part.ID), valueJSON, string(part.ValueKind),
			parentID, string(part.CreatedBy), string(part.CurrentFrom),
		); err != nil {
			return err
		}
	}
	return nil
}

// RebuildProjection recomputes the derived tables from the authoritative
// event ledger. It is O(ledger) and intended for recovery only.
func (s *Store) RebuildProjection() error {
	tx, err := s.writer.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := rebuildProjectionTx(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func rebuildProjectionTx(tx *sql.Tx) error {
	if _, err := tx.Exec(`DELETE FROM tickets`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM parts_current`); err != nil {
		return err
	}
	events, err := loadEventsTx(tx)
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
		if err := upsertTicketDerivedTx(tx, ticketID, streamEvents, 0); err != nil {
			return err
		}
	}
	return nil
}

// ensureDerivedFresh guarantees the derived tables match the authoritative
// event ledger on open. It rebuilds them when rows are missing or stale, which
// can happen when a binary predating migration 0004 appended events (ticket
// #51). Events remain the source of truth, so rebuilding derived data is safe.
func ensureDerivedFresh(db *sql.DB) error {
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
		return rebuildDerived(db)
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
	if len(heads) != ticketCount {
		return rebuildDerived(db)
	}
	for entity, head := range heads {
		var dbHead sql.NullString
		if err := db.QueryRow(`SELECT head_event FROM tickets WHERE ticket_id = ?`, entity).Scan(&dbHead); err != nil {
			return err
		}
		if !dbHead.Valid || dbHead.String != head {
			return rebuildDerived(db)
		}
	}
	return nil
}

func rebuildDerived(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := rebuildProjectionTx(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// verifyDerivedVsLedger compares every ticket's derived rows against a fold
// of its authoritative events.
func verifyDerivedVsLedger(tx *sql.Tx, byStream map[string][]model.Event) error {
	for key, streamEvents := range byStream {
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
		err = tx.QueryRow(`SELECT title, status FROM tickets WHERE ticket_id = ?`, string(ticketID)).Scan(&dbTitle, &dbStatus)
		if err == sql.ErrNoRows {
			return fmt.Errorf("derived ticket row missing for %s", ticketID)
		}
		if err != nil {
			return err
		}
		if dbTitle != title || dbStatus != status {
			return fmt.Errorf("derived ticket mismatch for %s: title %q/%q status %q/%q", ticketID, dbTitle, title, dbStatus, status)
		}
		rows, err := tx.Query(`SELECT path, value_json, parent_id FROM parts_current WHERE ticket_id = ?`, string(ticketID))
		if err != nil {
			return err
		}
		gotParts := make(map[string][2]any)
		for rows.Next() {
			var path string
			var valueJSON any
			var parentID any
			if err := rows.Scan(&path, &valueJSON, &parentID); err != nil {
				rows.Close()
				return err
			}
			gotParts[path] = [2]any{valueJSON, parentID}
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
			if got[0] != wantValue || got[1] != wantParent {
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

func eventsByIDsJSON(tx *sql.Tx, idsJSON string) ([]model.Event, error) {
	var ids []string
	if err := json.Unmarshal([]byte(idsJSON), &ids); err != nil {
		return nil, err
	}
	events := make([]model.Event, 0, len(ids))
	for _, id := range ids {
		var raw string
		var aliasSeq uint64
		err := tx.QueryRow(`SELECT event_json, alias_seq FROM events WHERE id = ?`, id).Scan(&raw, &aliasSeq)
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

func nextSequenceTx(tx *sql.Tx, streamKind, streamEntity string) (uint64, error) {
	var next uint64
	err := tx.QueryRow(
		`SELECT next_sequence FROM streams WHERE stream_kind = ? AND stream_entity = ?`,
		streamKind, streamEntity,
	).Scan(&next)
	if err == sql.ErrNoRows {
		return 1, nil
	}
	return next, err
}

func allocateSequenceTx(tx *sql.Tx, streamKind, streamEntity string, count uint64) (uint64, error) {
	var newValue uint64
	err := tx.QueryRow(
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

func checkPreconditions(events []model.Event, preconditions []Precondition) error {
	for _, precondition := range preconditions {
		if precondition.TargetEntity == "" || precondition.ExpectedCurrentEvent == "" {
			continue
		}
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
	return nil
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
