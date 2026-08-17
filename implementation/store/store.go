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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"

	"github.com/ravinsharma7/missis/implementation/model"
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

	// appendCommitHook is test-only fault injection. When non-nil it runs just
	// before the append transaction commits, and its error is returned in place
	// of a commit failure. Production code never sets it.
	appendCommitHook func() error
}

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Open(path string) (*Store, error) {
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
	// The SQLite file is created with the process umask; tighten it to
	// owner-only so ticket content is not world-readable by default.
	if err := os.Chmod(path, 0o600); err != nil {
		writer.Close()
		return nil, err
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
	return &Store{writer: writer, reader: reader}, nil
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

func newULID(prefix string) string {
	return prefix + ":" + ulid.Make().String()
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
		storeID = newULID("store")
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
			return outcome, alias, err
		}
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
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

	existing, err := loadEventsTx(tx)
	if err != nil {
		return AppendOutcome{}, 0, err
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
			event.ID = model.EventID(newULID("event"))
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
	rows, err := s.reader.Query(
		`SELECT event_json, alias_seq FROM events WHERE stream_kind = ? AND stream_entity = ? ORDER BY sequence ASC`,
		string(model.KindTicket), string(ticketID),
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
	events, err := s.LoadTicketEvents(ticketID)
	if err != nil {
		return nil, err
	}
	return model.CurrentProjection(events, ticketID, effectiveAt)
}

func (s *Store) BitemporalProjection(ticketID model.TicketID, effectiveAt, knownAt time.Time) (*model.Projection, error) {
	events, err := s.LoadTicketEvents(ticketID)
	if err != nil {
		return nil, err
	}
	return model.BitemporalProjection(events, ticketID, effectiveAt, knownAt)
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
