package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/model"
)

var ErrArtifactConflict = errors.New("artifact metadata conflict")

// ArtifactRecord is the SQLite metadata index for one immutable artifact.
// Backend identifies where the bytes are stored; it is not part of the
// content-addressed identity.
type ArtifactRecord struct {
	Ref        string
	Algorithm  string
	Digest     string
	MediaType  string
	Size       int64
	Backend    string
	RecordedAt time.Time
}

type ArtifactReferenceUsage struct {
	Ref       string   `json:"ref"`
	Managed   bool     `json:"managed"`
	EventIDs  []string `json:"event_ids"`
	Locations []string `json:"locations"`
}

// ListAcceptedArtifactReferences replays the typed event model and reports
// exactly which accepted events/fields reference each local artifact.
func (s *Store) ListAcceptedArtifactReferences(ctx context.Context) ([]ArtifactReferenceUsage, error) {
	events, err := s.LoadEventsContext(ctx)
	if err != nil {
		return nil, err
	}
	byRef := make(map[string]*ArtifactReferenceUsage)
	for _, event := range events {
		occurrences, err := model.AcceptedArtifactReferences(event)
		if err != nil {
			return nil, err
		}
		for _, occurrence := range occurrences {
			key := fmt.Sprintf("%t:%s", occurrence.Managed, occurrence.Ref)
			usage := byRef[key]
			if usage == nil {
				usage = &ArtifactReferenceUsage{Ref: occurrence.Ref, Managed: occurrence.Managed}
				byRef[key] = usage
			}
			usage.EventIDs = append(usage.EventIDs, string(occurrence.EventID))
			usage.Locations = append(usage.Locations, occurrence.Location)
		}
	}
	refs := make([]string, 0, len(byRef))
	for key := range byRef {
		refs = append(refs, key)
	}
	sort.Strings(refs)
	result := make([]ArtifactReferenceUsage, 0, len(refs))
	for _, ref := range refs {
		result = append(result, *byRef[ref])
	}
	return result, nil
}

// RecordArtifact indexes metadata after the artifact backend has durably
// committed the bytes. It never writes artifact bytes and is safe to retry for
// the same immutable metadata.
func (s *Store) RecordArtifact(ctx context.Context, record ArtifactRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := normalizeArtifactRecord(&record); err != nil {
		return err
	}
	ref, err := artifact.ParseRef(record.Ref)
	if err != nil {
		return err
	}

	var existing ArtifactRecord
	err = scanArtifactRecord(s.reader.QueryRowContext(ctx, `
		SELECT ref, algorithm, digest, media_type, size, backend, recorded_at
		FROM artifacts WHERE ref = ?`, ref.String()), &existing)
	if err == nil {
		if existing.Algorithm != record.Algorithm || existing.Digest != record.Digest || existing.Size != record.Size {
			return fmt.Errorf("%w: %s", ErrArtifactConflict, ref)
		}
		if existing.MediaType != "" && record.MediaType != "" && existing.MediaType != record.MediaType {
			return fmt.Errorf("%w: media type %q versus %q for %s", ErrArtifactConflict, existing.MediaType, record.MediaType, ref)
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = s.writer.ExecContext(ctx, `
		INSERT INTO artifacts (ref, algorithm, digest, media_type, size, backend, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, ref.String(), record.Algorithm, record.Digest,
		record.MediaType, record.Size, record.Backend, record.RecordedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return nil
}

func normalizeArtifactRecord(record *ArtifactRecord) error {
	ref, err := artifact.ParseRef(record.Ref)
	if err != nil {
		return err
	}
	if record.Algorithm == "" {
		record.Algorithm = "sha256"
	}
	if record.Algorithm != "sha256" || record.Digest != strings.TrimPrefix(ref.String(), "artifact:sha256:") || record.Size < 0 {
		return fmt.Errorf("%w: invalid identity or size for %s", ErrArtifactConflict, ref)
	}
	if strings.TrimSpace(record.Backend) == "" {
		return fmt.Errorf("artifact backend is required")
	}
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now().UTC()
	}
	return nil
}

func insertArtifactTxContext(ctx context.Context, tx *sql.Tx, record ArtifactRecord) error {
	if err := normalizeArtifactRecord(&record); err != nil {
		return err
	}
	var existing ArtifactRecord
	err := scanArtifactRecord(tx.QueryRowContext(ctx, `
		SELECT ref, algorithm, digest, media_type, size, backend, recorded_at
		FROM artifacts WHERE ref = ?`, record.Ref), &existing)
	if err == nil {
		if existing.Algorithm != record.Algorithm || existing.Digest != record.Digest || existing.Size != record.Size {
			return fmt.Errorf("%w: %s", ErrArtifactConflict, record.Ref)
		}
		if existing.MediaType != "" && record.MediaType != "" && existing.MediaType != record.MediaType {
			return fmt.Errorf("%w: media type %q versus %q for %s", ErrArtifactConflict, existing.MediaType, record.MediaType, record.Ref)
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO artifacts (ref, algorithm, digest, media_type, size, backend, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, record.Ref, record.Algorithm, record.Digest,
		record.MediaType, record.Size, record.Backend, record.RecordedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetArtifact(ctx context.Context, ref string) (ArtifactRecord, error) {
	if err := ctx.Err(); err != nil {
		return ArtifactRecord{}, err
	}
	parsed, err := artifact.ParseRef(ref)
	if err != nil {
		return ArtifactRecord{}, err
	}
	var record ArtifactRecord
	err = scanArtifactRecord(s.reader.QueryRowContext(ctx, `
		SELECT ref, algorithm, digest, media_type, size, backend, recorded_at
		FROM artifacts WHERE ref = ?`, parsed.String()), &record)
	return record, err
}

func (s *Store) ListArtifacts(ctx context.Context) ([]ArtifactRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.reader.QueryContext(ctx, `
		SELECT ref, algorithm, digest, media_type, size, backend, recorded_at
		FROM artifacts ORDER BY ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ArtifactRecord
	for rows.Next() {
		var record ArtifactRecord
		if err := scanArtifactRecord(rows, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// RebuildArtifactIndexContext replaces only the rebuildable artifact metadata
// index. Callers must derive records by replaying accepted event semantics and
// verifying immutable object bytes first. Accepted events are never changed.
func (s *Store) RebuildArtifactIndexContext(ctx context.Context, records []ArtifactRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts`); err != nil {
		return err
	}
	for _, record := range records {
		if err := insertArtifactTxContext(ctx, tx, record); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type artifactScanner interface {
	Scan(dest ...any) error
}

func scanArtifactRecord(scanner artifactScanner, record *ArtifactRecord) error {
	var recordedAt string
	if err := scanner.Scan(&record.Ref, &record.Algorithm, &record.Digest, &record.MediaType, &record.Size, &record.Backend, &recordedAt); err != nil {
		return err
	}
	if recordedAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, recordedAt)
		if err != nil {
			return err
		}
		record.RecordedAt = parsed
	}
	return nil
}
