package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/ravinsharma7/missis/implementation/model"
)

var ErrConflict = errors.New("optimistic concurrency conflict")

type Precondition struct {
	TargetEntity       string
	ExpectedCurrentEvent model.EventID
}

type AppendOutcome struct {
	Replayed bool
	Events   []model.Event
}

type TicketSummary struct {
	Ref        string
	ID         model.TicketID
	Title      string
	Status     string
	RecordedAt time.Time
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("store path is empty")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS streams (
			stream_kind TEXT NOT NULL,
			stream_entity TEXT NOT NULL,
			next_sequence INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (stream_kind, stream_entity)
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			alias_seq INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT NOT NULL UNIQUE,
			stream_kind TEXT NOT NULL,
			stream_entity TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			event_json TEXT NOT NULL,
			UNIQUE (stream_kind, stream_entity, sequence)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_stream ON events(stream_kind, stream_entity)`,
		`CREATE TABLE IF NOT EXISTS idempotency (
			key TEXT PRIMARY KEY,
			event_ids_json TEXT NOT NULL,
			result_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AppendBatch(events []model.Event, idempotencyKey string, preconditions []Precondition, result any) (AppendOutcome, error) {
	if len(events) == 0 {
		return AppendOutcome{}, fmt.Errorf("event batch is empty")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return AppendOutcome{}, err
	}
	defer tx.Rollback()

	if idempotencyKey != "" {
		var resultJSON string
		var eventIDsJSON string
		err := tx.QueryRow(`SELECT result_json, event_ids_json FROM idempotency WHERE key = ?`, idempotencyKey).Scan(&resultJSON, &eventIDsJSON)
		if err == nil {
			if result != nil && resultJSON != "" {
				if err := json.Unmarshal([]byte(resultJSON), result); err != nil {
					return AppendOutcome{}, err
				}
			}
			events, err := eventsByIDsJSON(tx, eventIDsJSON)
			if err != nil {
				return AppendOutcome{}, err
			}
			return AppendOutcome{Replayed: true, Events: events}, nil
		}
		if err != sql.ErrNoRows {
			return AppendOutcome{}, err
		}
	}

	if events[0].Stream.Kind == "" || events[0].Stream.Entity == "" {
		return AppendOutcome{}, fmt.Errorf("event stream is required")
	}
	streamKind := string(events[0].Stream.Kind)
	streamEntity := events[0].Stream.Entity
	for _, event := range events {
		if string(event.Stream.Kind) != streamKind || event.Stream.Entity != streamEntity {
			return AppendOutcome{}, fmt.Errorf("batch contains multiple streams")
		}
	}

	existing, err := loadEventsTx(tx)
	if err != nil {
		return AppendOutcome{}, err
	}

	if err := checkPreconditions(existing, preconditions); err != nil {
		return AppendOutcome{}, err
	}

	nextSequence, err := nextSequenceTx(tx, streamKind, streamEntity)
	if err != nil {
		return AppendOutcome{}, err
	}

	now := time.Now().UTC()
	appended := make([]model.Event, 0, len(events))
	running := append([]model.Event(nil), existing...)

	for i := range events {
		event := events[i]
		if event.ID == "" {
			event.ID = model.EventID("event:" + uuid.NewString())
		}
		if event.Sequence == 0 {
			event.Sequence = nextSequence + uint64(i)
		}
		if event.RecordedAt.IsZero() {
			event.RecordedAt = now
		}
		if event.EffectiveAt.IsZero() {
			event.EffectiveAt = event.RecordedAt
		}
		if err := model.ValidateAppend(running, event); err != nil {
			return AppendOutcome{}, err
		}
		eventJSON, err := json.Marshal(event)
		if err != nil {
			return AppendOutcome{}, err
		}
		if _, err := tx.Exec(
			`INSERT INTO events (id, stream_kind, stream_entity, sequence, event_json) VALUES (?, ?, ?, ?, ?)`,
			event.ID, streamKind, streamEntity, event.Sequence, eventJSON,
		); err != nil {
			return AppendOutcome{}, err
		}
		var aliasSeq uint64
		if err := tx.QueryRow(`SELECT alias_seq FROM events WHERE id = ?`, event.ID).Scan(&aliasSeq); err != nil {
			return AppendOutcome{}, err
		}
		event.AliasSeq = aliasSeq
		running = append(running, event)
		appended = append(appended, event)
	}

	newNext := nextSequence + uint64(len(events))
	if _, err := tx.Exec(
		`INSERT INTO streams (stream_kind, stream_entity, next_sequence) VALUES (?, ?, ?)
		 ON CONFLICT(stream_kind, stream_entity) DO UPDATE SET next_sequence = excluded.next_sequence`,
		streamKind, streamEntity, newNext,
	); err != nil {
		return AppendOutcome{}, err
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
				return AppendOutcome{}, err
			}
			resultJSON = string(raw)
		}
		if _, err := tx.Exec(
			`INSERT INTO idempotency (key, event_ids_json, result_json, created_at) VALUES (?, ?, ?, ?)`,
			idempotencyKey, string(idsJSON), resultJSON, now.Format(time.RFC3339Nano),
		); err != nil {
			return AppendOutcome{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return AppendOutcome{}, err
	}
	return AppendOutcome{Replayed: false, Events: appended}, nil
}

func (s *Store) LoadEvents() ([]model.Event, error) {
	return loadEventsTx(s.db)
}

func (s *Store) LoadTicketEvents(ticketID model.TicketID) ([]model.Event, error) {
	rows, err := s.db.Query(
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
	err = s.db.QueryRow(`SELECT event_json FROM events WHERE alias_seq = ?`, number).Scan(&raw)
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
		summary := TicketSummary{ID: ticketID, Ref: "#" + shortID(ticketID)}
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
