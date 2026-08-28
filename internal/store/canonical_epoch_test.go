package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

func TestNewStorePersistsExactCanonicalAcceptedBytes(t *testing.T) {
	t.Parallel()
	const (
		wantCodec = "missis-event-canonical-json-v1"
		wantEpoch = "canonical-event-chain-v1"
	)
	path := filepath.Join(t.TempDir(), "missis.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	at := time.Date(2026, 8, 28, 7, 8, 9, 120_000_000, time.FixedZone("fixture", 8*60*60))
	proposed := model.Event{
		ID:          "event:canonical-format7",
		Stream:      model.Ref{Kind: model.KindRun, Entity: "run:canonical-format7"},
		Operation:   model.OpObserveEffect,
		Target:      model.Ref{Kind: model.KindPart, Entity: "observation:canonical-format7"},
		Value:       model.Value{Kind: model.ValueKindJSON, Text: "probe", Data: map[string]any{"html": "<exact>", "number": float64(42)}},
		RecordedAt:  at,
		EffectiveAt: at,
		Actor:       model.ActorRef{Kind: "facility", ID: "spy-testing"},
	}
	outcome, err := s.AppendBatch([]model.Event{proposed}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted := outcome.Events[0]
	wantBytes, err := model.CanonicalEventBytesV1(accepted)
	if err != nil {
		t.Fatal(err)
	}
	wantSum := sha256.Sum256(wantBytes)
	wantContentHash := "sha256:" + hex.EncodeToString(wantSum[:])

	var codec string
	var storedBytes []byte
	var contentHash, eventEpoch, activeEpoch string
	if err := s.reader.QueryRow(`
		SELECT e.record_codec,e.accepted_bytes,e.content_hash,h.integrity_epoch,m.integrity_epoch
		FROM events e
		JOIN event_hashes h ON h.event_id=e.id
		JOIN store_meta m ON m.singleton=1
		WHERE e.id=?`, accepted.ID).Scan(&codec, &storedBytes, &contentHash, &eventEpoch, &activeEpoch); err != nil {
		t.Fatal(err)
	}
	if codec != wantCodec {
		t.Fatalf("record codec = %q, want %q", codec, wantCodec)
	}
	if string(storedBytes) != string(wantBytes) {
		t.Fatalf("accepted bytes differ\n got: %s\nwant: %s", storedBytes, wantBytes)
	}
	if contentHash != wantContentHash {
		t.Fatalf("content hash = %q, want %q", contentHash, wantContentHash)
	}
	if eventEpoch != wantEpoch || activeEpoch != wantEpoch {
		t.Fatalf("epochs event=%q active=%q, want %q", eventEpoch, activeEpoch, wantEpoch)
	}
}

func TestFormat6MigrationPreservesHistoryAndReceiptBindsFirstCanonicalEvent(t *testing.T) {
	t.Parallel()
	source := filepath.Join("testdata", "compatibility", "revision-0006", "fixture.db")
	rawDB, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "format6.db")
	if err := os.WriteFile(path, rawDB, 0o600); err != nil {
		t.Fatal(err)
	}

	beforeDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var beforeStoreID, beforeHead, beforeLastEventJSON, beforeLastHash string
	var beforeCount int64
	if err := beforeDB.QueryRow(`SELECT store_id,head_hash FROM store_meta WHERE singleton=1`).Scan(&beforeStoreID, &beforeHead); err != nil {
		t.Fatal(err)
	}
	if err := beforeDB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	if err := beforeDB.QueryRow(`SELECT e.event_json,h.hash FROM events e JOIN event_hashes h ON h.event_id=e.id ORDER BY e.alias_seq DESC LIMIT 1`).Scan(&beforeLastEventJSON, &beforeLastHash); err != nil {
		t.Fatal(err)
	}
	if err := beforeDB.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); !errors.Is(err, ErrStoreMigrationRequired) {
		t.Fatalf("format-6 Open error = %v, want migration required", err)
	}
	backup := filepath.Join(t.TempDir(), "pre-format7.db")
	report, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, backup)
	if err != nil {
		t.Fatal(err)
	}
	if report.FromFormat != 6 || report.ToFormat != 7 || report.ToStoreID != beforeStoreID || report.BackupPath != backup {
		t.Fatalf("migration report = %#v", report)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var afterStoreID, afterHead, activeEpoch, afterLastEventJSON, afterLastHash string
	if err := s.reader.QueryRow(`SELECT store_id,head_hash,integrity_epoch FROM store_meta WHERE singleton=1`).Scan(&afterStoreID, &afterHead, &activeEpoch); err != nil {
		t.Fatal(err)
	}
	if err := s.reader.QueryRow(`SELECT e.event_json,h.hash FROM events e JOIN event_hashes h ON h.event_id=e.id ORDER BY e.alias_seq DESC LIMIT 1`).Scan(&afterLastEventJSON, &afterLastHash); err != nil {
		t.Fatal(err)
	}
	var historicalCanonicalRows int64
	if err := s.reader.QueryRow(`SELECT COUNT(*) FROM events WHERE record_codec IS NOT NULL OR accepted_bytes IS NOT NULL OR content_hash IS NOT NULL`).Scan(&historicalCanonicalRows); err != nil {
		t.Fatal(err)
	}
	if afterStoreID != beforeStoreID || afterHead != beforeHead || afterLastHash != beforeLastHash || afterLastEventJSON != beforeLastEventJSON || historicalCanonicalRows != 0 || activeEpoch != globalJSONIntegrityEpochV1 {
		t.Fatalf("migration changed history: store=%q/%q head=%q/%q hash=%q/%q canonical_rows=%d epoch=%q",
			beforeStoreID, afterStoreID, beforeHead, afterHead, beforeLastHash, afterLastHash, historicalCanonicalRows, activeEpoch)
	}

	at := time.Date(2026, 8, 28, 9, 10, 11, 0, time.UTC)
	first := model.Event{
		ID: "event:first-format7", Stream: model.Ref{Kind: model.KindRun, Entity: "run:first-format7"},
		Operation: model.OpObserveEffect, Target: model.Ref{Kind: model.KindPart, Entity: "observation:first-format7"},
		Value:      model.Value{Kind: model.ValueKindJSON, Text: "probe", Data: map[string]any{"ok": true}},
		RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "facility", ID: "spy-testing"},
	}
	outcome, err := s.AppendBatch([]model.Event{first}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted := outcome.Events[0]
	var receiptSourceHead, receiptFirstEvent, receiptFirstHead, receiptContentHash, receiptEpoch string
	var receiptSourceCount int64
	var receiptCursor uint64
	if err := s.reader.QueryRow(`SELECT source_head_digest,source_event_count,activation_after_alias_seq,first_event_id,first_head_digest,first_content_hash,target_integrity_epoch FROM integrity_epoch_transition_receipts`).Scan(
		&receiptSourceHead, &receiptSourceCount, &receiptCursor, &receiptFirstEvent, &receiptFirstHead, &receiptContentHash, &receiptEpoch,
	); err != nil {
		t.Fatal(err)
	}
	var firstHead, firstContentHash string
	if err := s.reader.QueryRow(`SELECT h.hash,e.content_hash FROM event_hashes h JOIN events e ON e.id=h.event_id WHERE e.id=?`, accepted.ID).Scan(&firstHead, &firstContentHash); err != nil {
		t.Fatal(err)
	}
	if receiptSourceHead != beforeHead || receiptSourceCount != beforeCount || receiptCursor == 0 ||
		receiptFirstEvent != string(accepted.ID) || receiptFirstHead != firstHead || receiptContentHash != firstContentHash ||
		receiptEpoch != canonicalEventIntegrityEpochV1 {
		t.Fatalf("transition receipt does not bind boundary: source_head=%q count=%d cursor=%d first=%q head=%q content=%q epoch=%q",
			receiptSourceHead, receiptSourceCount, receiptCursor, receiptFirstEvent, receiptFirstHead, receiptContentHash, receiptEpoch)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatal(err)
	}
	genesisEpoch, err := s.GenesisIntegrityEpochContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	headEpoch, err := s.HeadIntegrityEpochContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if genesisEpoch != globalJSONIntegrityEpochV1 || headEpoch != canonicalEventIntegrityEpochV1 {
		t.Fatalf("mixed history epochs genesis=%q head=%q", genesisEpoch, headEpoch)
	}
}

func TestFormatMigrationReceiptTamperingFailsClosed(t *testing.T) {
	mutations := []struct {
		name      string
		statement string
		wantError string
	}{
		{name: "indexed-field", statement: `UPDATE store_format_migration_receipts SET source_head_integrity_epoch='forged-epoch-v1'`, wantError: "indexed fields disagree"},
		{name: "receipt-bytes", statement: `UPDATE store_format_migration_receipts SET receipt_bytes=receipt_bytes || X'20'`, wantError: "digest mismatch"},
	}
	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()
			rawDB, err := os.ReadFile(filepath.Join("testdata", "compatibility", "revision-0006", "fixture.db"))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "receipt-tamper.db")
			if err := os.WriteFile(path, rawDB, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, filepath.Join(t.TempDir(), "pre-format7.db")); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(mutation.statement); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil || !strings.Contains(err.Error(), mutation.wantError) {
				t.Fatalf("Open error = %v, want %q", err, mutation.wantError)
			}
		})
	}
}

func TestCanonicalAcceptedBytesSurviveReopenAndBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.db")
	backup := filepath.Join(dir, "backup.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	event := model.Event{
		ID: "event:backup-canonical", Stream: model.Ref{Kind: model.KindRun, Entity: "run:backup-canonical"},
		Operation: model.OpObserveEffect, Target: model.Ref{Kind: model.KindPart, Entity: "observation:backup-canonical"},
		Value:      model.Value{Kind: model.ValueKindJSON, Text: "probe", Data: map[string]any{"value": "<preserved>"}},
		RecordedAt: time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC),
		Actor: model.ActorRef{Kind: "facility", ID: "spy-testing"},
	}
	if _, err := s.AppendBatch([]model.Event{event}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := s.reader.QueryRow(`SELECT accepted_bytes FROM events WHERE id=?`, event.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, backup} {
		reopened, err := Open(candidate)
		if err != nil {
			t.Fatalf("open %s: %v", candidate, err)
		}
		var after []byte
		if err := reopened.reader.QueryRow(`SELECT accepted_bytes FROM events WHERE id=?`, event.ID).Scan(&after); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			reopened.Close()
			t.Fatalf("accepted bytes changed in %s", candidate)
		}
		if err := reopened.CheckConsistency(); err != nil {
			reopened.Close()
			t.Fatal(err)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanonicalAcceptedRecordCorruptionFailsWithSpecificCause(t *testing.T) {
	mutations := []struct {
		name      string
		statement string
		wantError string
	}{
		{name: "accepted-bytes", statement: `UPDATE events SET accepted_bytes=CAST(replace(CAST(accepted_bytes AS TEXT),'preserved','tampered!') AS BLOB)`, wantError: "content digest mismatch"},
		{name: "content-digest", statement: `UPDATE events SET content_hash='sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'`, wantError: "content digest mismatch"},
		{name: "unknown-codec", statement: `UPDATE events SET record_codec='future-record-codec-v9'`, wantError: "unsupported record codec \"future-record-codec-v9\"; exact bytes preserved"},
	}
	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "corrupt.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			event := model.Event{
				ID: model.EventID("event:corruption-" + mutation.name), Stream: model.Ref{Kind: model.KindRun, Entity: "run:corruption-" + mutation.name},
				Operation: model.OpObserveEffect, Target: model.Ref{Kind: model.KindPart, Entity: "observation:corruption-" + mutation.name},
				Value:      model.Value{Kind: model.ValueKindJSON, Text: "probe", Data: map[string]any{"value": "preserved"}},
				RecordedAt: time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC),
				Actor: model.ActorRef{Kind: "facility", ID: "spy-testing"},
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
			if _, err := db.Exec(mutation.statement); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil || !strings.Contains(err.Error(), mutation.wantError) {
				t.Fatalf("Open error = %v, want %q", err, mutation.wantError)
			}
		})
	}
}

func TestMissingIntegrityEpochReceiptFailsClosed(t *testing.T) {
	t.Parallel()
	rawDB, err := os.ReadFile(filepath.Join("testdata", "compatibility", "revision-0006", "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "missing-receipt.db")
	if err := os.WriteFile(path, rawDB, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, filepath.Join(t.TempDir(), "pre-format7.db")); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	event := model.Event{
		ID: "event:missing-transition", Stream: model.Ref{Kind: model.KindRun, Entity: "run:missing-transition"},
		Operation: model.OpObserveEffect, Target: model.Ref{Kind: model.KindPart, Entity: "observation:missing-transition"},
		Value:      model.Value{Kind: model.ValueKindJSON, Text: "probe", Data: map[string]any{"ok": true}},
		RecordedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
		Actor: model.ActorRef{Kind: "facility", ID: "spy-testing"},
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
	if _, err := db.Exec(`DELETE FROM integrity_epoch_transition_receipts`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "want 1") {
		t.Fatalf("Open error = %v, want missing transition receipt", err)
	}
}

func TestConcurrentFirstFormat7AppendsCreateOneTransitionReceipt(t *testing.T) {
	t.Parallel()
	rawDB, err := os.ReadFile(filepath.Join("testdata", "compatibility", "revision-0006", "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "concurrent-transition.db")
	if err := os.WriteFile(path, rawDB, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, filepath.Join(t.TempDir(), "pre-format7.db")); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	start := make(chan struct{})
	errorsByWorker := make(chan error, 2)
	var wg sync.WaitGroup
	for worker := 1; worker <= 2; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			id := fmt.Sprintf("%d", worker)
			at := time.Date(2026, 8, 28, 13, 0, worker, 0, time.UTC)
			event := model.Event{
				ID: model.EventID("event:concurrent-transition-" + id), Stream: model.Ref{Kind: model.KindRun, Entity: "run:concurrent-transition-" + id},
				Operation: model.OpObserveEffect, Target: model.Ref{Kind: model.KindPart, Entity: "observation:concurrent-transition-" + id},
				Value:      model.Value{Kind: model.ValueKindJSON, Text: "probe", Data: map[string]any{"worker": float64(worker)}},
				RecordedAt: at, EffectiveAt: at, Actor: model.ActorRef{Kind: "test", ID: "worker-" + id},
			}
			_, err := s.AppendBatchContext(context.Background(), []model.Event{event}, "", nil, nil)
			errorsByWorker <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		if err != nil {
			t.Fatal(err)
		}
	}
	var receiptCount, canonicalCount int
	if err := s.reader.QueryRow(`SELECT COUNT(*) FROM integrity_epoch_transition_receipts`).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := s.reader.QueryRow(`SELECT COUNT(*) FROM event_hashes WHERE integrity_epoch=?`, canonicalEventIntegrityEpochV1).Scan(&canonicalCount); err != nil {
		t.Fatal(err)
	}
	if receiptCount != 1 || canonicalCount != 2 {
		t.Fatalf("transition receipts=%d canonical events=%d, want 1/2", receiptCount, canonicalCount)
	}
	if err := s.CheckConsistency(); err != nil {
		t.Fatal(err)
	}
}
