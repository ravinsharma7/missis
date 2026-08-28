package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

func createStoreThroughMigration(t *testing.T, path, through string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() > through {
			continue
		}
		data, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, entry.Name(), "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func migrateTestStore(t *testing.T, path string) {
	t.Helper()
	backup := filepath.Join(t.TempDir(), "pre-migration.db")
	if _, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, backup); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
}

func TestInspectImplicitStoreFormatRevisions(t *testing.T) {
	// covers PH1-FMT-001
	t.Parallel()
	for _, tc := range []struct {
		name      string
		migration string
		want      int
	}{
		{name: "implicit-revision-1", migration: "0005_artifacts.sql", want: 1},
		{name: "ordered", migration: "0006_ordered_parts.sql", want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missis.db")
			db := createStoreThroughMigration(t, path, tc.migration)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := inspectStoreFormat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("format revision = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestOpenMigratesFormatWithoutChangingLedger(t *testing.T) {
	// covers PH1-FMT-001
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missis.db")
	db := createStoreThroughMigration(t, path, "0006_ordered_parts.sql")
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	event := model.Event{
		ID: "event:format-fixture", Stream: model.Ref{Kind: model.KindTicket, Entity: "ticket:format-fixture"}, Sequence: 1,
		Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: "ticket:format-fixture"},
		RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "format-fixture"},
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO streams(stream_kind, stream_entity, next_sequence) VALUES ('ticket','ticket:format-fixture',2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events(id, stream_kind, stream_entity, sequence, event_json) VALUES (?,?,?,?,?)`, event.ID, event.Stream.Kind, event.Stream.Entity, 1, raw); err != nil {
		t.Fatal(err)
	}
	event.AliasSeq = 1
	head := computeEventHash(event, "")
	if _, err := db.Exec(`INSERT INTO store_meta(singleton, store_id, head_hash, updated_at) VALUES (1,'store:format-fixture',?,?)`, head, at.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO event_hashes(event_id, previous_hash, hash) VALUES (?,'',?)`, event.ID, head); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); !errors.Is(err, ErrStoreMigrationRequired) {
		t.Fatalf("Open error = %v, want migration required", err)
	}
	migrateTestStore(t, path)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.FormatRevision(); err != nil || got != CurrentStoreFormatRevision {
		t.Fatalf("format revision = %d, err=%v", got, err)
	}
	if got, err := s.HeadHash(); err != nil || got != head {
		t.Fatalf("head = %q, err=%v, want %q", got, err, head)
	}
	var after string
	if err := s.reader.QueryRow(`SELECT event_json FROM events WHERE id=?`, event.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != string(raw) {
		t.Fatalf("migration rewrote event JSON\n got: %s\nwant: %s", after, raw)
	}
}

func TestOpenMovesV2UnboundIdempotencyReceiptToTombstone(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missis.db")
	db := createStoreThroughMigration(t, path, "0007_store_format_revision.sql")
	if _, err := db.Exec(`INSERT INTO store_meta(singleton, store_id, head_hash, updated_at, format_revision) VALUES (1,'store:v2-idempotency','','2026-01-01T00:00:00Z',2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO idempotency(key, event_ids_json, result_json, created_at) VALUES ('v2-key','[]','{"ref":"#1"}','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	migrateTestStore(t, path)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.FormatRevision(); err != nil || got != CurrentStoreFormatRevision {
		t.Fatalf("format revision = %d, err=%v, want %d", got, err, CurrentStoreFormatRevision)
	}
	var activeCount int
	if err := s.reader.QueryRow(`SELECT COUNT(*) FROM idempotency WHERE key = 'v2-key'`).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 0 {
		t.Fatalf("format-v2 unbound receipt remained active: count=%d", activeCount)
	}
	var reason string
	if err := s.reader.QueryRow(`SELECT reason FROM idempotency_key_tombstones WHERE key = 'v2-key'`).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "format-v2-unbound-request" {
		t.Fatalf("tombstone reason = %q", reason)
	}
	proposed, err := ComputeIdempotencyRequestHashV1(map[string]string{"operation": "new"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithIdempotencyRequestHash(context.Background(), proposed)
	if replayed, err := s.LookupIdempotencyContext(ctx, "v2-key", nil); replayed || !errors.Is(err, ErrIdempotencyMismatch) {
		t.Fatalf("format-v2 guarded lookup replayed=%t err=%v, want mismatch", replayed, err)
	}
	var rawResult map[string]any
	if replayed, err := s.LookupIdempotency("v2-key", &rawResult); err != nil || !replayed || rawResult["ref"] != "#1" {
		t.Fatalf("tombstone audit lookup replayed=%t result=%v err=%v", replayed, rawResult, err)
	}
}

func TestRevision2FixtureMigrationTombstonesEveryUnboundKey(t *testing.T) {
	t.Parallel()
	source := filepath.Join("testdata", "compatibility", "revision-0002", "fixture.db")
	rawDB, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.db")
	if err := os.WriteFile(path, rawDB, 0o600); err != nil {
		t.Fatal(err)
	}

	migrateTestStore(t, path)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var active, tombstoned int
	if err := s.reader.QueryRow(`SELECT COUNT(*) FROM idempotency`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := s.reader.QueryRow(`SELECT COUNT(*) FROM idempotency_key_tombstones`).Scan(&tombstoned); err != nil {
		t.Fatal(err)
	}
	if active != 0 || tombstoned != 4 {
		t.Fatalf("migrated receipt counts active=%d tombstoned=%d, want 0 and 4", active, tombstoned)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatalf("migrated fixture consistency: %v", err)
	}
}

func TestUnsupportedFormatFailsBeforeWALOrIntegrity(t *testing.T) {
	// covers PH1-FMT-001
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missis.db")
	db := createStoreThroughMigration(t, path, "0008_idempotency_request_hash.sql")
	if _, err := db.Exec(`INSERT INTO store_meta(singleton, store_id, head_hash, updated_at, format_revision) VALUES (1,'store:future','deliberately-invalid','2026-01-01T00:00:00Z',?)`, CurrentStoreFormatRevision+1); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := Open(path)
	if !errors.Is(err, ErrIncompatibleStoreFormat) {
		t.Fatalf("Open error = %v, want incompatible format", err)
	}
	var detail *IncompatibleStoreFormatError
	if !errors.As(err, &detail) || detail.Found != CurrentStoreFormatRevision+1 || detail.Max != CurrentStoreFormatRevision {
		t.Fatalf("format error detail = %#v", detail)
	}
	probe, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	var journal string
	if err := probe.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if journal == "wal" {
		t.Fatal("incompatible open changed journal mode to WAL")
	}
}

func TestUnknownMigrationFailsClosed(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missis.db")
	db := createStoreThroughMigration(t, path, "0008_idempotency_request_hash.sql")
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES ('9999_future.sql','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if !errors.Is(err, ErrIncompatibleStoreFormat) {
		t.Fatalf("Open error = %v, want incompatible format", err)
	}
}

func TestNewStoreRecordsCurrentFormat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missis.db")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture path unexpectedly exists: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.FormatRevision(); err != nil || got != CurrentStoreFormatRevision {
		t.Fatalf("format revision = %d, err=%v", got, err)
	}
}

func TestOrderedFixtureDocumentsGlobalJSONChainV1Boundary(t *testing.T) {
	t.Parallel()
	source := filepath.Join("testdata", "compatibility", "revision-0002", "fixture.db")
	rawDB, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.db")
	if err := os.WriteFile(path, rawDB, 0o600); err != nil {
		t.Fatal(err)
	}
	migrateTestStore(t, path)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var alias uint64
	var rawEvent, previous, storedHash string
	if err := s.reader.QueryRow(`
		SELECT e.alias_seq, e.event_json, h.previous_hash, h.hash
		FROM events e JOIN event_hashes h ON h.event_id=e.id
		WHERE json_extract(e.event_json, '$.Value.OrderKey') IS NOT NULL
		ORDER BY e.alias_seq LIMIT 1`).Scan(&alias, &rawEvent, &previous, &storedHash); err != nil {
		t.Fatal(err)
	}
	var current model.Event
	if err := json.Unmarshal([]byte(rawEvent), &current); err != nil {
		t.Fatal(err)
	}
	current.AliasSeq = alias
	if got := computeEventHash(current, previous); got != storedHash {
		t.Fatalf("current model hash = %s, want stored %s", got, storedHash)
	}
	var preOrderKeyWire map[string]any
	if err := json.Unmarshal([]byte(rawEvent), &preOrderKeyWire); err != nil {
		t.Fatal(err)
	}
	value, ok := preOrderKeyWire["Value"].(map[string]any)
	if !ok {
		t.Fatal("fixture event Value is not an object")
	}
	delete(value, "OrderKey")
	preOrderKeyRaw, err := json.Marshal(preOrderKeyWire)
	if err != nil {
		t.Fatal(err)
	}
	var preOrderKey model.Event
	if err := json.Unmarshal(preOrderKeyRaw, &preOrderKey); err != nil {
		t.Fatal(err)
	}
	preOrderKey.AliasSeq = alias
	if got := computeEventHash(preOrderKey, previous); got == storedHash {
		t.Fatal("dropping OrderKey unexpectedly preserved the event hash; v0.2.1 regression boundary is no longer represented")
	}
}
