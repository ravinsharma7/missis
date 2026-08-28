package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/storeidentity"
)

func TestExplicitVersionedMigrationCreatesBackupIdentityAndReceipt(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "revision3.db")
	db := createStoreThroughMigration(t, path, "0008_idempotency_request_hash.sql")
	const oldID = "store:01MIGRATIONFIXTURE"
	if _, err := db.Exec(`INSERT INTO store_meta(singleton,store_id,head_hash,updated_at,format_revision) VALUES(1,?,'','2026-01-01T00:00:00Z',3)`, oldID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrStoreMigrationRequired) {
		t.Fatalf("Open error = %v, want migration required", err)
	}
	plan, err := PlanMigration(path, CurrentStoreFormatRevision)
	if err != nil {
		t.Fatal(err)
	}
	if plan.FromFormat != 3 || plan.ToFormat != CurrentStoreFormatRevision || plan.FromStoreID != oldID || !plan.RequiresBackup || !plan.ChangesStoreID {
		t.Fatalf("plan = %#v", plan)
	}
	backup := filepath.Join(t.TempDir(), "before-format5.db")
	report, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, backup)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "migrated" || report.FromStoreID != oldID || report.ToStoreID == oldID || report.ReceiptID == "" || report.BackupPath != backup {
		t.Fatalf("report = %#v", report)
	}
	if info, err := os.Stat(backup); err != nil || info.Size() == 0 {
		t.Fatalf("backup info=%v err=%v", info, err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	identity, err := s.IdentityInfoContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Scheme != storeidentity.Scheme || identity.StoreID != report.ToStoreID || identity.ArtifactNamespace != oldID {
		t.Fatalf("identity = %#v", identity)
	}
	if err := storeidentity.ValidateBinding(identity.StoreID, identity.DocumentBytes); err != nil {
		t.Fatal(err)
	}
	var receiptFrom, receiptTo, receiptDigest string
	if err := s.reader.QueryRow(`SELECT from_store_id,to_store_id,receipt_digest FROM store_identity_migration_receipts WHERE receipt_id=?`, report.ReceiptID).Scan(&receiptFrom, &receiptTo, &receiptDigest); err != nil {
		t.Fatal(err)
	}
	if receiptFrom != oldID || receiptTo != report.ToStoreID || receiptDigest != report.ReceiptDigest {
		t.Fatalf("receipt from=%q to=%q digest=%q", receiptFrom, receiptTo, receiptDigest)
	}
}

func TestResumeMigrationReusesOnlyVerifiedBoundBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revision3.db")
	db := createStoreThroughMigration(t, path, "0008_idempotency_request_hash.sql")
	const oldID = "store:01ROLLOUTRECOVERY"
	if _, err := db.Exec(`INSERT INTO store_meta(singleton,store_id,head_hash,updated_at,format_revision) VALUES(1,?,'','2026-01-01T00:00:00Z',3)`, oldID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "rollout-backup.db")
	if err := createPreMigrationBackup(context.Background(), path, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, backup); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("ordinary apply adopted existing backup: %v", err)
	}
	lease, err := AcquireExclusiveLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	report, err := ResumeMigrationWithLease(context.Background(), path, CurrentStoreFormatRevision, backup, lease)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "migrated" || report.FromFormat != 3 || report.ToFormat != CurrentStoreFormatRevision {
		t.Fatalf("report = %#v", report)
	}

	otherPath := filepath.Join(t.TempDir(), "other-revision3.db")
	other := createStoreThroughMigration(t, otherPath, "0008_idempotency_request_hash.sql")
	if _, err := other.Exec(`INSERT INTO store_meta(singleton,store_id,head_hash,updated_at,format_revision) VALUES(1,'store:OTHER','','2026-01-01T00:00:00Z',3)`); err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	otherLease, err := AcquireExclusiveLease(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	defer otherLease.Close()
	if _, err := ResumeMigrationWithLease(context.Background(), otherPath, CurrentStoreFormatRevision, backup, otherLease); err == nil || !strings.Contains(err.Error(), "backup generation mismatch") {
		t.Fatalf("unrelated backup accepted: %v", err)
	}
}

func TestMigrationTargetIsMandatoryAndVersionSpecific(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "current.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanMigration(path, CurrentStoreFormatRevision+1); err == nil || !strings.Contains(err.Error(), "targets format 6") {
		t.Fatalf("plan error = %v", err)
	}
	plan, err := PlanMigration(path, CurrentStoreFormatRevision)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiresBackup || plan.ChangesStoreID || plan.FromFormat != CurrentStoreFormatRevision {
		t.Fatalf("current plan = %#v", plan)
	}
}

func TestTamperedIdentityFailsBeforeNormalOpenConfiguresWAL(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "tampered.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE store_identity_v1 SET document_bytes = document_bytes || X'00' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path + "-wal")
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "invalid store identity") {
		t.Fatalf("Open error = %v", err)
	}
	if info, err := os.Stat(path + "-wal"); err == nil && info.Size() != 0 {
		t.Fatalf("normal open wrote WAL bytes before identity rejection: size=%d", info.Size())
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect WAL after identity rejection: %v", err)
	}
}

func TestExplicitVersionedWritableForkCreatesNewIdentityAndLineage(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "writable-copy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	originID, err := s.StoreID()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanWritableFork(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Eligible || !plan.RequiresBackup || plan.FromStoreID != originID || plan.ToIdentityVersion != 1 {
		t.Fatalf("fork plan = %#v", plan)
	}
	backup := filepath.Join(t.TempDir(), "before-fork.db")
	report, err := ApplyWritableFork(context.Background(), path, 1, originID, backup)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "forked" || report.ToStoreID == originID || report.ReceiptID == "" || report.ReceiptDigest == "" {
		t.Fatalf("fork report = %#v", report)
	}
	forked, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer forked.Close()
	identity, err := forked.IdentityInfoContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.StoreID != report.ToStoreID || identity.ArtifactNamespace != report.ToStoreID {
		t.Fatalf("fork identity = %#v", identity)
	}
	if digest, err := forked.LatestIdentityLineageReceiptDigestContext(context.Background()); err != nil || digest != report.ReceiptDigest {
		t.Fatalf("lineage digest = %q err=%v", digest, err)
	}
	if err := forked.CheckConsistency(); err != nil {
		t.Fatal(err)
	}
	forkBackup := filepath.Join(t.TempDir(), "fork-backup.db")
	if err := forked.Backup(forkBackup); err != nil {
		t.Fatal(err)
	}
	forkBackupStore, err := Open(forkBackup)
	if err != nil {
		t.Fatal(err)
	}
	defer forkBackupStore.Close()
	if backupID, err := forkBackupStore.StoreID(); err != nil || backupID != report.ToStoreID {
		t.Fatalf("fork backup identity = %q err=%v", backupID, err)
	}
	if digest, err := forkBackupStore.LatestIdentityLineageReceiptDigestContext(context.Background()); err != nil || digest != report.ReceiptDigest {
		t.Fatalf("fork backup lineage digest = %q err=%v", digest, err)
	}
	backupStore, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer backupStore.Close()
	if backupID, err := backupStore.StoreID(); err != nil || backupID != originID {
		t.Fatalf("backup identity = %q err=%v", backupID, err)
	}
}

func TestWritableForkRequiresExactIdentityVersionAndSourceConfirmation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "copy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanWritableFork(path, 2); err == nil || !strings.Contains(err.Error(), "identity version 1") {
		t.Fatalf("target error = %v", err)
	}
	backup := filepath.Join(t.TempDir(), "before.db")
	if _, err := ApplyWritableFork(context.Background(), path, 1, "store:v1:sha256:"+strings.Repeat("0", 64), backup); err == nil || !strings.Contains(err.Error(), "source identity changed") {
		t.Fatalf("source confirmation error = %v", err)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup created before source confirmation: %v", err)
	}
}

func TestWritableForkCopiesIndexedArtifactIntoIndependentNamespace(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "artifact-copy.db")
	sourceRoot := filepath.Join(t.TempDir(), "source-artifacts")
	destinationRoot := filepath.Join(t.TempDir(), "child-artifacts")
	cas, err := artifact.NewLocalStore(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := cas.Put(context.Background(), bytes.NewBufferString("indexed artifact bytes"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	excluded, err := cas.Put(context.Background(), bytes.NewBufferString("valid but unreferenced"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	originID, err := s.StoreID()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(context.Background(), ArtifactRecord{Ref: metadata.Ref.String(), Algorithm: metadata.Algorithm, Digest: metadata.Digest, MediaType: metadata.MediaType, Size: metadata.Size, Backend: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanWritableFork(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Eligible || !plan.RequiresArtifactNamespaceFork || plan.ArtifactRecordCount != 1 || plan.BlockedReason != "" {
		t.Fatalf("artifact fork plan = %#v", plan)
	}
	plan, err = InspectWritableForkArtifactInventory(context.Background(), plan, sourceRoot)
	if err != nil || plan.ArtifactInventoryStatus != "verified" || plan.RequiredManagedObjectCount != 1 || plan.ExcludedSourceObjectCount != 1 || len(plan.ExcludedSourceObjectRefs) != 1 || plan.ExcludedSourceObjectRefs[0] != excluded.Ref.String() {
		t.Fatalf("full artifact fork plan = %#v err=%v", plan, err)
	}
	backup := filepath.Join(t.TempDir(), "before-fork.db")
	if _, err := ApplyWritableFork(context.Background(), path, 1, originID, backup); err == nil || !strings.Contains(err.Error(), "--source-artifact-root") {
		t.Fatalf("missing artifact roots error = %v", err)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid fork created backup: %v", err)
	}
	report, err := ApplyWritableForkWithOptions(context.Background(), path, 1, originID, backup, WritableForkOptions{
		SourceArtifactRoot: sourceRoot, DestinationArtifactRoot: destinationRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ToStoreID == originID {
		t.Fatalf("fork kept parent identity: %#v", report)
	}
	childCAS, err := artifact.OpenLocalStore(destinationRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := childCAS.Verify(context.Background(), metadata.Ref); err != nil || got != metadata {
		t.Fatalf("copied artifact = %#v err=%v", got, err)
	}
	inspection, err := InspectArtifactNamespaceFork(context.Background(), path, destinationRoot)
	if err != nil || inspection.Status != "complete" || inspection.CopiedObjectCount != 1 || inspection.ExcludedObjectCount != 1 {
		t.Fatalf("inspection = %#v err=%v", inspection, err)
	}
	if exists, err := childCAS.Exists(context.Background(), excluded.Ref); err != nil || exists {
		t.Fatalf("excluded object copied=%t err=%v", exists, err)
	}
	if _, err := cas.Verify(context.Background(), metadata.Ref); err != nil {
		t.Fatalf("source namespace changed: %v", err)
	}
}

func TestWritableForkReplaysAcceptedEventsWhenArtifactIndexIsMissing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "artifact-reference-only.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	artifactRef := model.Ref{Kind: model.KindArtifact, Entity: "artifact:sha256:" + strings.Repeat("b", 64)}
	event := model.Event{
		ID: "event:artifact-reference-only", Stream: model.Ref{Kind: model.KindTicket, Entity: "ticket:artifact-reference-only"},
		Operation: model.OpAttachEvidence, Target: model.Ref{Kind: model.KindPart, Entity: "part:evidence"},
		Value: model.Value{Kind: model.ValueKindEvidence, Ref: &artifactRef}, RecordedAt: now, EffectiveAt: now,
		Actor: model.ActorRef{Kind: "user", ID: "actor:test"},
	}
	if _, err := s.AppendBatch([]model.Event{event}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanWritableFork(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Eligible || !plan.RequiresArtifactNamespaceFork || plan.ArtifactRecordCount != 0 || plan.AcceptedArtifactReferenceEventCount != 1 || plan.ManagedCASReferenceOccurrences != 1 || plan.UnmanagedSourceReferenceOccurrences != 0 || plan.MissingArtifactIndexCount != 1 {
		t.Fatalf("artifact replay fork plan = %#v", plan)
	}
	sourceRoot := filepath.Join(t.TempDir(), "empty-source")
	if _, err := artifact.NewLocalStore(sourceRoot); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "must-not-exist.db")
	destinationRoot := filepath.Join(t.TempDir(), "child-artifacts")
	_, err = ApplyWritableForkWithOptions(context.Background(), path, 1, plan.FromStoreID, backup, WritableForkOptions{
		SourceArtifactRoot: sourceRoot, DestinationArtifactRoot: destinationRoot,
	})
	if err == nil || !strings.Contains(err.Error(), "artifact-index reconciliation") || !strings.Contains(err.Error(), "missing_index_rows=1") {
		t.Fatalf("unreconciled accepted artifact error = %v", err)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup exists after failed artifact verification: %v", err)
	}
	inspection, err := InspectArtifactNamespaceFork(context.Background(), path, destinationRoot)
	if err != nil || inspection.Status != "absent" {
		t.Fatalf("blocked-copy inspection = %#v err=%v", inspection, err)
	}
}

func TestWritableForkReportsUnmanagedSourceIdentitySeparately(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "unmanaged-source-reference.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	event := model.Event{
		ID: "event:unmanaged-source-reference", Stream: model.Ref{Kind: model.KindTicket, Entity: "ticket:unmanaged-source-reference"},
		Operation: model.OpAttachEvidence, Target: model.Ref{Kind: model.KindPart, Entity: "part:evidence"},
		Value: model.Value{Kind: model.ValueKindEvidence, Ref: &model.Ref{Kind: model.KindArtifact, Entity: "artifact:specs/report.md"}}, RecordedAt: now, EffectiveAt: now,
		Actor: model.ActorRef{Kind: "user", ID: "actor:test"},
	}
	if _, err := s.AppendBatch([]model.Event{event}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanWritableFork(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Eligible || !plan.RequiresArtifactNamespaceFork || plan.AcceptedArtifactReferenceEventCount != 1 || plan.ManagedCASReferenceOccurrences != 0 || plan.UnmanagedSourceReferenceOccurrences != 1 {
		t.Fatalf("unmanaged source fork plan = %#v", plan)
	}
	destinationRoot := filepath.Join(t.TempDir(), "child-artifacts")
	report, err := ApplyWritableForkWithOptions(context.Background(), path, 1, plan.FromStoreID, filepath.Join(t.TempDir(), "before.db"), WritableForkOptions{
		DestinationArtifactRoot: destinationRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectArtifactNamespaceFork(context.Background(), path, destinationRoot)
	if err != nil || inspection.Status != "complete" || inspection.UnmanagedReferenceCount != 1 || inspection.CopiedObjectCount != 0 || inspection.ToStoreID != report.ToStoreID {
		t.Fatalf("unmanaged fork inspection = %#v err=%v", inspection, err)
	}
}

func TestWritableForkRecoversAfterNamespacePublication(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "recoverable-fork.db")
	sourceRoot := filepath.Join(t.TempDir(), "source-artifacts")
	destinationRoot := filepath.Join(t.TempDir(), "child-artifacts")
	cas, err := artifact.NewLocalStore(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := cas.Put(context.Background(), bytes.NewBufferString("recoverable bytes"), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	originID, err := s.StoreID()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(context.Background(), ArtifactRecord{Ref: metadata.Ref.String(), Algorithm: metadata.Algorithm, Digest: metadata.Digest, Size: metadata.Size, Backend: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "before.db")
	injected := errors.New("stop after publication")
	_, err = ApplyWritableForkWithOptions(context.Background(), path, 1, originID, backup, WritableForkOptions{
		SourceArtifactRoot: sourceRoot, DestinationArtifactRoot: destinationRoot,
		Fault: func(stage string) error {
			if stage == "after-namespace-publish" {
				return injected
			}
			return nil
		},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("injected failure = %v", err)
	}
	inspection, err := InspectArtifactNamespaceFork(context.Background(), path, destinationRoot)
	if err != nil || inspection.Status != "prepared-awaiting-database-commit" || inspection.DatabaseStoreID != originID {
		t.Fatalf("prepared inspection = %#v err=%v", inspection, err)
	}
	report, err := ApplyWritableForkWithOptions(context.Background(), path, 1, originID, backup, WritableForkOptions{
		SourceArtifactRoot: sourceRoot, DestinationArtifactRoot: destinationRoot, ExecutionMode: WritableForkRecoverV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err = InspectArtifactNamespaceFork(context.Background(), path, destinationRoot)
	if err != nil || inspection.Status != "complete" || inspection.ToStoreID != report.ToStoreID {
		t.Fatalf("recovered inspection = %#v err=%v", inspection, err)
	}
}

func TestWritableForkDurableFaultBoundariesAreInspectable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		stage      string
		wantStatus string
		committed  bool
	}{
		{"after-object-copy", "copy-incomplete", false},
		{"after-artifact-copy", "copy-incomplete", false},
		{"after-manifest", "manifest-written-copy-incomplete", false},
		{"after-completion-marker", "prepared-awaiting-namespace-publication", false},
		{"after-namespace-publish", "prepared-awaiting-database-commit", false},
		{"before-database-commit", "prepared-awaiting-database-commit", false},
		{"after-database-commit", "complete", true},
	}
	for _, test := range cases {
		test := test
		t.Run(test.stage, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "fork.db")
			sourceRoot := filepath.Join(t.TempDir(), "source")
			destinationRoot := filepath.Join(t.TempDir(), "destination")
			cas, err := artifact.NewLocalStore(sourceRoot)
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := cas.Put(context.Background(), bytes.NewBufferString("fault boundary bytes"), "application/octet-stream")
			if err != nil {
				t.Fatal(err)
			}
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			originID, err := s.StoreID()
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RecordArtifact(context.Background(), ArtifactRecord{Ref: metadata.Ref.String(), Algorithm: metadata.Algorithm, Digest: metadata.Digest, Size: metadata.Size, Backend: "local"}); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(t.TempDir(), "before.db")
			injected := errors.New("fault boundary")
			_, err = ApplyWritableForkWithOptions(context.Background(), path, 1, originID, backup, WritableForkOptions{
				SourceArtifactRoot: sourceRoot, DestinationArtifactRoot: destinationRoot,
				Fault: func(stage string) error {
					if stage == test.stage {
						return injected
					}
					return nil
				},
			})
			if !errors.Is(err, injected) {
				t.Fatalf("injected error = %v", err)
			}
			inspection, err := InspectArtifactNamespaceFork(context.Background(), path, destinationRoot)
			if err != nil || inspection.Status != test.wantStatus {
				t.Fatalf("inspection = %#v err=%v, want %s", inspection, err, test.wantStatus)
			}
			if test.committed {
				return
			}
			report, err := ApplyWritableForkWithOptions(context.Background(), path, 1, originID, backup, WritableForkOptions{
				SourceArtifactRoot: sourceRoot, DestinationArtifactRoot: destinationRoot, ExecutionMode: WritableForkRecoverV1,
			})
			if err != nil {
				t.Fatal(err)
			}
			inspection, err = InspectArtifactNamespaceFork(context.Background(), path, destinationRoot)
			if err != nil || inspection.Status != "complete" || inspection.ToStoreID != report.ToStoreID {
				t.Fatalf("recovered inspection = %#v err=%v", inspection, err)
			}
		})
	}
}

func TestWritableForkRejectsUnsafeRootsAndReportsCorruptRequiredObject(t *testing.T) {
	t.Parallel()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	cas, err := artifact.NewLocalStore(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := cas.Put(context.Background(), bytes.NewBufferString("original bytes"), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateArtifactForkRoots(sourceRoot, filepath.Join(sourceRoot, "child"), true); err == nil || !strings.Contains(err.Error(), "non-nested") {
		t.Fatalf("nested root error = %v", err)
	}
	symlinkTarget := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(symlinkTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkRoot := filepath.Join(t.TempDir(), "destination-link")
	if err := os.Symlink(symlinkTarget, symlinkRoot); err == nil {
		if err := validateArtifactForkRoots(sourceRoot, symlinkRoot, true); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("symlink root error = %v", err)
		}
	}

	path := filepath.Join(t.TempDir(), "corrupt-source.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(context.Background(), ArtifactRecord{Ref: metadata.Ref.String(), Algorithm: metadata.Algorithm, Digest: metadata.Digest, Size: metadata.Size, Backend: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	digest := strings.TrimPrefix(metadata.Ref.String(), "artifact:sha256:")
	dataPath := filepath.Join(sourceRoot, "sha256", digest[:2], digest[2:4], digest)
	if err := os.WriteFile(dataPath, []byte("changed!bytes!"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanWritableFork(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = InspectWritableForkArtifactInventory(context.Background(), plan, sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Eligible || plan.ArtifactInventoryStatus != "integrity-failure" || len(plan.ArtifactIntegrityIssues) == 0 || !strings.Contains(strings.Join(plan.ArtifactIntegrityIssues, " "), metadata.Ref.String()) {
		t.Fatalf("corrupt source plan = %#v", plan)
	}
}

func TestWritableForkRetainsFormatMigrationAsAncestor(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "revision3-copy.db")
	db := createStoreThroughMigration(t, path, "0008_idempotency_request_hash.sql")
	if _, err := db.Exec(`INSERT INTO store_meta(singleton,store_id,head_hash,updated_at,format_revision) VALUES(1,'store:01FORKMIGRATION','','2026-01-01T00:00:00Z',3)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	migration, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, filepath.Join(t.TempDir(), "pre-format6.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyWritableFork(context.Background(), path, 1, migration.ToStoreID, filepath.Join(t.TempDir(), "pre-fork.db")); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CheckConsistency(); err != nil {
		t.Fatal(err)
	}
}

func TestFormat4To5MigrationPreservesIdentityAndRecordsReceipt(t *testing.T) {
	// covers PH1-FMT-002
	t.Parallel()
	path := filepath.Join(t.TempDir(), "revision4.db")
	source := filepath.Join("testdata", "compatibility", "revision-0004", "fixture.db")
	input, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := readCurrentIdentityReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanMigration(path, CurrentStoreFormatRevision)
	if err != nil {
		t.Fatal(err)
	}
	if plan.FromFormat != 4 || plan.ToFormat != CurrentStoreFormatRevision || plan.ChangesStoreID || !plan.RequiresBackup || plan.FromStoreID != identity.StoreID {
		t.Fatalf("format 4 plan = %#v", plan)
	}
	report, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, filepath.Join(t.TempDir(), "pre-format6.db"))
	if err != nil {
		t.Fatal(err)
	}
	if report.ToStoreID != identity.StoreID || !strings.HasPrefix(report.ReceiptID, "format-migration:") {
		t.Fatalf("format report = %#v", report)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CheckConsistency(); err != nil {
		t.Fatal(err)
	}
	if digest, err := s.LatestFormatMigrationReceiptDigestContext(context.Background()); err != nil || digest != report.ReceiptDigest {
		t.Fatalf("format receipt digest=%q err=%v", digest, err)
	}
}

func TestFormat5To6MigrationPreservesIdentityAndAddsArtifactForkIndex(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "revision5.db")
	input, err := os.ReadFile(filepath.Join("testdata", "compatibility", "revision-0005", "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := readCurrentIdentityReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanMigration(path, CurrentStoreFormatRevision)
	if err != nil {
		t.Fatal(err)
	}
	if plan.FromFormat != 5 || plan.ToFormat != 6 || plan.ChangesStoreID || !plan.RequiresBackup {
		t.Fatalf("format 5 plan = %#v", plan)
	}
	report, err := ApplyMigration(context.Background(), path, CurrentStoreFormatRevision, filepath.Join(t.TempDir(), "pre-format6.db"))
	if err != nil {
		t.Fatal(err)
	}
	if report.ToStoreID != before.StoreID {
		t.Fatalf("identity changed during format-only migration: %#v", report)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var table string
	if err := s.reader.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='artifact_namespace_forks'`).Scan(&table); err != nil || table != "artifact_namespace_forks" {
		t.Fatalf("artifact namespace fork index = %q err=%v", table, err)
	}
}
