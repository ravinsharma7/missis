package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

func TestOpenVerifiedReadSnapshotFailsStopOnCorruptChain(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "corrupt.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	event := model.Event{
		ID: "event:peer-corrupt", Stream: model.Ref{Kind: model.KindTicket, Entity: "ticket:peer-corrupt"},
		Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: "ticket:peer-corrupt"},
		RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"},
	}
	if _, err := s.AppendBatch([]model.Event{event}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE event_hashes SET hash='changed' WHERE event_id=?`, event.ID); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVerifiedReadSnapshot(context.Background(), path); err == nil || !strings.Contains(err.Error(), "integrity verification failed") {
		t.Fatalf("corrupt peer open error = %v", err)
	}
}

func TestOpenVerifiedReadSnapshotPreservesDatabaseAndCreatesOnlySQLiteSidecars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "peer.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.ReadFile(path + ".maintenance.lock")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := OpenVerifiedReadSnapshot(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.IdentityInfoContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lockAfter, err := os.ReadFile(path + ".maintenance.lock")
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("read-only peer changed database bytes")
	}
	if sha256.Sum256(lockBefore) != sha256.Sum256(lockAfter) {
		t.Fatal("read-only peer changed coordination lock contents")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "peer.db", "peer.db.maintenance.lock", "peer.db-wal", "peer.db-shm":
		default:
			t.Fatalf("read-only peer created unexpected path %q", entry.Name())
		}
	}
}

func TestOpenVerifiedReadSnapshotDoesNotChangeLiveDatabaseWALOrLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "live.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 8, 28, 10, 30, 0, 0, time.UTC)
	event := model.Event{ID: "event:live-peer", Stream: model.Ref{Kind: model.KindTicket, Entity: "ticket:live-peer"}, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: "ticket:live-peer"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}}
	if _, err := s.AppendBatch([]model.Event{event}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	before := hashFiles(t, path, path+"-wal", path+".maintenance.lock")
	snapshot, err := OpenVerifiedReadSnapshot(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	after := hashFiles(t, path, path+"-wal", path+".maintenance.lock")
	for file, digest := range before {
		if after[file] != digest {
			t.Fatalf("read-only peer changed %s", file)
		}
	}
}

func TestOpenVerifiedReadSnapshotRejectsOldFormatWithoutMutation(t *testing.T) {
	t.Parallel()
	source := filepath.Join("testdata", "compatibility", "revision-0004", "fixture.db")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "old.db")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".maintenance.lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(raw)
	_, err = OpenVerifiedReadSnapshot(context.Background(), path)
	var migration *StoreMigrationRequiredError
	if !errors.As(err, &migration) || migration.Found != 4 || migration.Target != CurrentStoreFormatRevision {
		t.Fatalf("old peer error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != before {
		t.Fatal("old-format inspection changed database")
	}
}

func TestOpenVerifiedReadSnapshotRequiresExistingCoordinationLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "peer.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".maintenance.lock"
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVerifiedReadSnapshot(context.Background(), path); !errors.Is(err, ErrMaintenanceLock) {
		t.Fatalf("open error = %v, want maintenance lock", err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only peer created lock: %v", err)
	}
}

func TestReadSnapshotCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "peer.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := OpenVerifiedReadSnapshot(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func hashFiles(t *testing.T, paths ...string) map[string][sha256.Size]byte {
	t.Helper()
	result := make(map[string][sha256.Size]byte, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = sha256.Sum256(raw)
	}
	return result
}
