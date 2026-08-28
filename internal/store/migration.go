package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/storeidentity"
)

type MigrationPlan struct {
	Path               string `json:"path"`
	FromFormat         int    `json:"from_format"`
	ToFormat           int    `json:"to_format"`
	FromStoreID        string `json:"from_store_id,omitempty"`
	FromIdentityScheme string `json:"from_identity_scheme,omitempty"`
	SourceHeadDigest   string `json:"source_head_digest"`
	SourceEventCount   int64  `json:"source_event_count"`
	IntegrityEpoch     string `json:"integrity_epoch"`
	ArtifactNamespace  string `json:"artifact_namespace,omitempty"`
	RequiresBackup     bool   `json:"requires_backup"`
	ChangesStoreID     bool   `json:"changes_store_id"`
}

type MigrationReport struct {
	MigrationPlan
	Status        string `json:"status"`
	ToStoreID     string `json:"to_store_id"`
	ReceiptID     string `json:"receipt_id,omitempty"`
	ReceiptDigest string `json:"receipt_digest,omitempty"`
	BackupPath    string `json:"backup_path"`
}

const CurrentStoreIdentityVersion = 1

type WritableForkPlan struct {
	Path                                string   `json:"path"`
	FromStoreID                         string   `json:"from_store_id"`
	FromIdentityScheme                  string   `json:"from_identity_scheme"`
	SourceArtifactNamespace             string   `json:"source_artifact_namespace"`
	SourceHeadDigest                    string   `json:"source_head_digest"`
	ToIdentityVersion                   int      `json:"to_identity_version"`
	FormatRevision                      int      `json:"format_revision"`
	EventCount                          int64    `json:"event_count"`
	ArtifactRecordCount                 int64    `json:"artifact_record_count"`
	AcceptedArtifactReferenceEventCount int64    `json:"accepted_artifact_reference_event_count"`
	ManagedCASReferenceOccurrences      int64    `json:"managed_cas_reference_occurrences"`
	UnmanagedSourceReferenceOccurrences int64    `json:"unmanaged_source_reference_occurrences"`
	MissingArtifactIndexCount           int64    `json:"missing_artifact_index_count"`
	RequiresArtifactNamespaceFork       bool     `json:"requires_artifact_namespace_fork"`
	ArtifactForkProtocol                string   `json:"artifact_fork_protocol,omitempty"`
	ReceiptVersion                      string   `json:"receipt_version"`
	ArtifactInventoryStatus             string   `json:"artifact_inventory_status"`
	RequiredManagedObjectCount          int      `json:"required_managed_object_count"`
	ExcludedSourceObjectCount           int      `json:"excluded_source_object_count"`
	ExcludedSourceObjectRefs            []string `json:"excluded_source_object_refs,omitempty"`
	ArtifactIntegrityIssues             []string `json:"artifact_integrity_issues,omitempty"`
	Eligible                            bool     `json:"eligible"`
	BlockedReason                       string   `json:"blocked_reason,omitempty"`
	RequiresBackup                      bool     `json:"requires_backup"`
}

type acceptedArtifactInventory struct {
	EventCount                          int64
	ManagedCASReferenceOccurrences      int64
	UnmanagedSourceReferenceOccurrences int64
	ManagedRefs                         map[string]struct{}
}

type WritableForkReport struct {
	WritableForkPlan
	Status        string `json:"status"`
	ToStoreID     string `json:"to_store_id"`
	ReceiptID     string `json:"receipt_id"`
	ReceiptDigest string `json:"receipt_digest"`
	BackupPath    string `json:"backup_path"`
}

type identityMigrationReceiptV1 struct {
	Version                  string `json:"version"`
	FromStoreID              string `json:"from_store_id"`
	FromIdentityScheme       string `json:"from_identity_scheme"`
	ToStoreID                string `json:"to_store_id"`
	ToIdentityScheme         string `json:"to_identity_scheme"`
	SourceHeadDigest         string `json:"source_head_digest"`
	SourceHeadIntegrityEpoch string `json:"source_head_integrity_epoch"`
	SourceEventCount         int64  `json:"source_event_count"`
	SourceFormatRevision     int    `json:"source_format_revision"`
	TargetFormatRevision     int    `json:"target_format_revision"`
	ArtifactNamespace        string `json:"artifact_namespace"`
	BackupDatabaseSHA256     string `json:"backup_database_sha256"`
	MigratedAt               string `json:"migrated_at"`
}

type formatMigrationReceiptV1 struct {
	Version                  string `json:"version"`
	StoreID                  string `json:"store_id"`
	SourceHeadDigest         string `json:"source_head_digest"`
	SourceHeadIntegrityEpoch string `json:"source_head_integrity_epoch"`
	SourceEventCount         int64  `json:"source_event_count"`
	SourceFormatRevision     int    `json:"source_format_revision"`
	TargetFormatRevision     int    `json:"target_format_revision"`
	BackupDatabaseSHA256     string `json:"backup_database_sha256"`
	MigratedAt               string `json:"migrated_at"`
}

type writableForkReceiptV1 struct {
	Version                    string `json:"version"`
	FromStoreID                string `json:"from_store_id"`
	FromIdentityScheme         string `json:"from_identity_scheme"`
	FromIdentityDocument       []byte `json:"from_identity_document"`
	FromIdentityDocumentDigest string `json:"from_identity_document_digest"`
	FromHeadDigest             string `json:"from_head_digest"`
	FromHeadIntegrityEpoch     string `json:"from_head_integrity_epoch"`
	FromEventCount             int64  `json:"from_event_count"`
	FromFormatRevision         int    `json:"from_format_revision"`
	ToStoreID                  string `json:"to_store_id"`
	ToIdentityScheme           string `json:"to_identity_scheme"`
	ToIdentityDocumentDigest   string `json:"to_identity_document_digest"`
	ArtifactDisposition        string `json:"artifact_disposition"`
	ArtifactNamespace          string `json:"artifact_namespace"`
	BackupDatabaseSHA256       string `json:"backup_database_sha256"`
	ForkedAt                   string `json:"forked_at"`
}

func PlanWritableFork(path string, identityVersion int) (WritableForkPlan, error) {
	if identityVersion != CurrentStoreIdentityVersion {
		return WritableForkPlan{}, fmt.Errorf("unsupported identity target %d; this binary targets identity version %d", identityVersion, CurrentStoreIdentityVersion)
	}
	clean := filepath.Clean(path)
	revision, err := inspectStoreFormat(clean)
	if err != nil {
		return WritableForkPlan{}, err
	}
	if revision != CurrentStoreFormatRevision {
		return WritableForkPlan{}, &StoreMigrationRequiredError{Found: revision, Target: CurrentStoreFormatRevision, Path: clean}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(clean)+"?mode=ro")
	if err != nil {
		return WritableForkPlan{}, err
	}
	defer db.Close()
	var plan WritableForkPlan
	plan.Path = clean
	plan.ToIdentityVersion = identityVersion
	plan.FormatRevision = revision
	plan.RequiresBackup = true
	if err := db.QueryRow(`SELECT store_id,identity_scheme,artifact_namespace FROM store_identity_v1 WHERE singleton=1`).Scan(&plan.FromStoreID, &plan.FromIdentityScheme, &plan.SourceArtifactNamespace); err != nil {
		return WritableForkPlan{}, err
	}
	if err := db.QueryRow(`SELECT head_hash FROM store_meta WHERE singleton=1`).Scan(&plan.SourceHeadDigest); err != nil {
		return WritableForkPlan{}, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&plan.EventCount); err != nil {
		return WritableForkPlan{}, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&plan.ArtifactRecordCount); err != nil {
		return WritableForkPlan{}, err
	}
	indexedRefs := map[string]struct{}{}
	artifactRows, err := db.Query(`SELECT ref FROM artifacts ORDER BY ref`)
	if err != nil {
		return WritableForkPlan{}, err
	}
	for artifactRows.Next() {
		var ref string
		if err := artifactRows.Scan(&ref); err != nil {
			artifactRows.Close()
			return WritableForkPlan{}, err
		}
		indexedRefs[ref] = struct{}{}
	}
	if err := artifactRows.Close(); err != nil {
		return WritableForkPlan{}, err
	}
	inventory, err := inventoryAcceptedArtifactReferences(db)
	if err != nil {
		return WritableForkPlan{}, err
	}
	plan.AcceptedArtifactReferenceEventCount = inventory.EventCount
	plan.ManagedCASReferenceOccurrences = inventory.ManagedCASReferenceOccurrences
	plan.UnmanagedSourceReferenceOccurrences = inventory.UnmanagedSourceReferenceOccurrences
	for ref := range inventory.ManagedRefs {
		if _, indexed := indexedRefs[ref]; !indexed {
			plan.MissingArtifactIndexCount++
		}
	}
	plan.RequiresArtifactNamespaceFork = plan.ArtifactRecordCount > 0 ||
		plan.ManagedCASReferenceOccurrences > 0 || plan.UnmanagedSourceReferenceOccurrences > 0
	plan.ReceiptVersion = "store-identity-fork-v1"
	if plan.RequiresArtifactNamespaceFork {
		plan.ArtifactForkProtocol = "artifact-namespace-fork-v1"
		plan.ReceiptVersion = "store-identity-fork-v2"
	}
	plan.Eligible = plan.MissingArtifactIndexCount == 0
	plan.ArtifactInventoryStatus = "not-inspected"
	if !plan.RequiresArtifactNamespaceFork {
		plan.ArtifactInventoryStatus = "not-required"
	}
	if !plan.Eligible {
		plan.BlockedReason = "accepted managed artifact references are missing derived index rows; run artifacts rebuild-index-copy and fork the verified replacement database"
	}
	return plan, nil
}

func inventoryAcceptedArtifactReferences(db *sql.DB) (acceptedArtifactInventory, error) {
	rows, err := db.Query(`SELECT event_json FROM events ORDER BY alias_seq`)
	if err != nil {
		return acceptedArtifactInventory{}, err
	}
	defer rows.Close()
	inventory := acceptedArtifactInventory{ManagedRefs: map[string]struct{}{}}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return acceptedArtifactInventory{}, err
		}
		var event model.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return acceptedArtifactInventory{}, fmt.Errorf("decode accepted event while inventorying artifacts: %w", err)
		}
		occurrences, err := model.AcceptedArtifactReferences(event)
		if err != nil {
			return acceptedArtifactInventory{}, err
		}
		if len(occurrences) > 0 {
			inventory.EventCount++
		}
		for _, occurrence := range occurrences {
			if occurrence.Managed {
				inventory.ManagedCASReferenceOccurrences++
				inventory.ManagedRefs[occurrence.Ref] = struct{}{}
			} else {
				inventory.UnmanagedSourceReferenceOccurrences++
			}
		}
	}
	return inventory, rows.Err()
}

func ApplyWritableFork(ctx context.Context, path string, identityVersion int, expectedFromStoreID, backupPath string) (WritableForkReport, error) {
	return ApplyWritableForkWithOptions(ctx, path, identityVersion, expectedFromStoreID, backupPath, WritableForkOptions{})
}

func ApplyWritableForkWithOptions(ctx context.Context, path string, identityVersion int, expectedFromStoreID, backupPath string, options WritableForkOptions) (WritableForkReport, error) {
	if err := ctx.Err(); err != nil {
		return WritableForkReport{}, err
	}
	if strings.TrimSpace(expectedFromStoreID) == "" {
		return WritableForkReport{}, fmt.Errorf("--from-store-id is required when declaring a writable fork")
	}
	if strings.TrimSpace(backupPath) == "" {
		return WritableForkReport{}, fmt.Errorf("--backup is required when declaring a writable fork")
	}
	plan, err := PlanWritableFork(path, identityVersion)
	if err != nil {
		return WritableForkReport{}, err
	}
	if plan.FromStoreID != expectedFromStoreID {
		return WritableForkReport{}, fmt.Errorf("fork source identity changed: expected %q, found %q", expectedFromStoreID, plan.FromStoreID)
	}
	if !plan.Eligible {
		return WritableForkReport{}, fmt.Errorf("writable fork requires artifact-index reconciliation: %s (missing_index_rows=%d)", plan.BlockedReason, plan.MissingArtifactIndexCount)
	}
	if plan.RequiresArtifactNamespaceFork {
		if plan.ArtifactRecordCount > 0 || plan.ManagedCASReferenceOccurrences > 0 {
			if strings.TrimSpace(options.SourceArtifactRoot) == "" {
				return WritableForkReport{}, errors.New("--source-artifact-root is required because the fork must independently copy managed or indexed artifact bytes")
			}
		}
		if strings.TrimSpace(options.DestinationArtifactRoot) == "" {
			return WritableForkReport{}, errors.New("--destination-artifact-root is required because the fork must publish an independent artifact namespace")
		}
		if err := validateArtifactForkRoots(options.SourceArtifactRoot, options.DestinationArtifactRoot, plan.ArtifactRecordCount > 0 || plan.ManagedCASReferenceOccurrences > 0); err != nil {
			return WritableForkReport{}, err
		}
	}
	if options.ExecutionMode == "" {
		options.ExecutionMode = WritableForkApplyV1
	}
	if options.ExecutionMode != WritableForkApplyV1 && options.ExecutionMode != WritableForkRecoverV1 {
		return WritableForkReport{}, fmt.Errorf("unsupported writable fork execution mode %q", options.ExecutionMode)
	}
	backupPath = filepath.Clean(backupPath)
	backupExists := false
	if info, statErr := os.Stat(backupPath); statErr == nil {
		backupExists = true
		if info.Size() == 0 {
			return WritableForkReport{}, fmt.Errorf("fork backup is empty: %s", backupPath)
		}
		if options.ExecutionMode != WritableForkRecoverV1 {
			return WritableForkReport{}, fmt.Errorf("fork backup already exists: %s", backupPath)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return WritableForkReport{}, statErr
	}
	lease, err := AcquireExclusiveLease(plan.Path)
	if err != nil {
		return WritableForkReport{}, err
	}
	defer lease.Close()
	plan, err = PlanWritableFork(plan.Path, identityVersion)
	if err != nil {
		return WritableForkReport{}, err
	}
	if plan.FromStoreID != expectedFromStoreID {
		return WritableForkReport{}, fmt.Errorf("fork source identity changed under lease: expected %q, found %q", expectedFromStoreID, plan.FromStoreID)
	}
	if !plan.Eligible {
		return WritableForkReport{}, fmt.Errorf("writable fork requires artifact-index reconciliation: %s (missing_index_rows=%d)", plan.BlockedReason, plan.MissingArtifactIndexCount)
	}
	var artifactLeases []*Lease
	if plan.RequiresArtifactNamespaceFork {
		paths := []string{options.DestinationArtifactRoot}
		if strings.TrimSpace(options.SourceArtifactRoot) != "" {
			paths = append(paths, options.SourceArtifactRoot)
		}
		for i := range paths {
			absolute, absErr := filepath.Abs(filepath.Clean(paths[i]))
			if absErr != nil {
				return WritableForkReport{}, absErr
			}
			paths[i] = absolute
		}
		sort.Strings(paths)
		for index, artifactPath := range paths {
			if index > 0 && artifactPath == paths[index-1] {
				return WritableForkReport{}, errors.New("source and destination artifact roots must differ")
			}
			artifactLease, leaseErr := AcquireExclusiveLease(artifactPath)
			if leaseErr != nil {
				for _, held := range artifactLeases {
					_ = held.Close()
				}
				return WritableForkReport{}, leaseErr
			}
			artifactLeases = append(artifactLeases, artifactLease)
		}
		defer func() {
			for _, held := range artifactLeases {
				_ = held.Close()
			}
		}()
	}
	db, err := sql.Open("sqlite", plan.Path+"?_txlock=immediate")
	if err != nil {
		return WritableForkReport{}, err
	}
	defer db.Close()
	if err := configureDB(db); err != nil {
		return WritableForkReport{}, err
	}
	if err := migrate(db); err != nil {
		return WritableForkReport{}, err
	}
	if err := verifyPreMigrationHashes(ctx, db); err != nil {
		return WritableForkReport{}, fmt.Errorf("pre-fork integrity verification failed: %w", err)
	}
	if !backupExists {
		if err := createPreMigrationBackup(ctx, plan.Path, backupPath); err != nil {
			return WritableForkReport{}, err
		}
	}
	backupDigest, err := sha256File(backupPath)
	if err != nil {
		return WritableForkReport{}, fmt.Errorf("digest pre-fork backup: %w", err)
	}
	if err := verifyWritableForkBackup(ctx, backupPath, plan); err != nil {
		return WritableForkReport{}, fmt.Errorf("verify pre-fork backup: %w", err)
	}
	var operation artifactForkOperationV1
	var manifest artifactForkManifestV1
	var complete artifactForkCompleteV1
	var manifestDigest, completeDigest string
	if plan.RequiresArtifactNamespaceFork {
		operation, manifest, complete, manifestDigest, completeDigest, err = prepareArtifactNamespaceFork(ctx, db, plan, backupDigest, options)
		if err != nil {
			return WritableForkReport{}, err
		}
		plan.ArtifactInventoryStatus = "verified"
		plan.RequiredManagedObjectCount = manifest.CopiedObjectCount
		plan.ExcludedSourceObjectCount = manifest.ExcludedUnreferencedObjCount
		plan.ExcludedSourceObjectRefs = append([]string(nil), manifest.ExcludedUnreferencedObjects...)
	}
	var toStoreID, receiptID, receiptDigest string
	if err := invokeForkFault(options, "before-database-commit"); err != nil {
		return WritableForkReport{}, err
	}
	if plan.RequiresArtifactNamespaceFork {
		toStoreID, receiptID, receiptDigest, err = declareWritableForkV2(ctx, db, plan, backupDigest, operation, manifest, complete, manifestDigest, completeDigest)
	} else {
		toStoreID, receiptID, receiptDigest, err = declareWritableForkV1(ctx, db, plan, backupDigest)
	}
	if err != nil {
		return WritableForkReport{}, err
	}
	if err := invokeForkFault(options, "after-database-commit"); err != nil {
		return WritableForkReport{}, err
	}
	if err := db.Close(); err != nil {
		return WritableForkReport{}, err
	}
	if plan.RequiresArtifactNamespaceFork {
		inspection, inspectErr := InspectArtifactNamespaceFork(ctx, plan.Path, options.DestinationArtifactRoot)
		if inspectErr != nil || inspection.Status != "complete" {
			return WritableForkReport{}, fmt.Errorf("post-fork artifact namespace verification failed: status=%q issues=%v err=%v", inspection.Status, inspection.Issues, inspectErr)
		}
	}
	info, err := readCurrentIdentityReadOnly(plan.Path)
	if err != nil || info.StoreID != toStoreID {
		return WritableForkReport{}, fmt.Errorf("post-fork identity verification failed: store_id=%q err=%v", info.StoreID, err)
	}
	return WritableForkReport{WritableForkPlan: plan, Status: "forked", ToStoreID: toStoreID, ReceiptID: receiptID, ReceiptDigest: receiptDigest, BackupPath: backupPath}, nil
}

func verifyWritableForkBackup(ctx context.Context, path string, plan WritableForkPlan) error {
	revision, err := inspectStoreFormat(path)
	if err != nil {
		return err
	}
	if revision != plan.FormatRevision {
		return fmt.Errorf("backup format=%d, expected=%d", revision, plan.FormatRevision)
	}
	info, err := readCurrentIdentityReadOnly(path)
	if err != nil {
		return err
	}
	if info.StoreID != plan.FromStoreID || info.DocumentDigest == "" {
		return fmt.Errorf("backup store_id=%q, expected=%q", info.StoreID, plan.FromStoreID)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Clean(path))+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	if err := verifyPreMigrationHashes(ctx, db); err != nil {
		return err
	}
	var head string
	var count int64
	if err := db.QueryRowContext(ctx, `SELECT head_hash FROM store_meta WHERE singleton=1`).Scan(&head); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return err
	}
	if head != plan.SourceHeadDigest || count != plan.EventCount {
		return fmt.Errorf("backup snapshot changed: head=%q events=%d, expected head=%q events=%d", head, count, plan.SourceHeadDigest, plan.EventCount)
	}
	return nil
}

func PlanMigration(path string, target int) (MigrationPlan, error) {
	if target != CurrentStoreFormatRevision {
		return MigrationPlan{}, fmt.Errorf("unsupported migration target %d; this binary targets format %d", target, CurrentStoreFormatRevision)
	}
	clean := filepath.Clean(path)
	revision, err := inspectStoreFormat(clean)
	if err != nil {
		return MigrationPlan{}, err
	}
	plan := MigrationPlan{Path: clean, FromFormat: revision, ToFormat: target, RequiresBackup: revision < target, ChangesStoreID: revision < 4}
	if revision == target {
		source, err := readMigrationSource(clean)
		if err != nil {
			return MigrationPlan{}, err
		}
		info, err := readCurrentIdentityReadOnly(clean)
		if err != nil {
			return MigrationPlan{}, err
		}
		plan.FromStoreID, plan.FromIdentityScheme = info.StoreID, info.Scheme
		plan.SourceHeadDigest, plan.SourceEventCount = source.head, source.eventCount
		plan.IntegrityEpoch, plan.ArtifactNamespace = "global-json-chain-v1", info.ArtifactNamespace
		plan.RequiresBackup, plan.ChangesStoreID = false, false
		return plan, nil
	}
	source, err := readMigrationSource(clean)
	if err != nil {
		return MigrationPlan{}, err
	}
	plan.FromStoreID = source.storeID
	plan.SourceHeadDigest, plan.SourceEventCount = source.head, source.eventCount
	plan.IntegrityEpoch = "global-json-chain-v1"
	if revision >= 4 {
		info, identityErr := readCurrentIdentityReadOnly(clean)
		if identityErr != nil {
			return MigrationPlan{}, identityErr
		}
		if identityErr := validateIdentityInfo(info); identityErr != nil {
			return MigrationPlan{}, identityErr
		}
		plan.FromStoreID, plan.FromIdentityScheme = info.StoreID, info.Scheme
		plan.ArtifactNamespace = info.ArtifactNamespace
	} else if source.storeID != "" {
		plan.FromIdentityScheme = "missis-ulid-v1"
	}
	return plan, nil
}

func ApplyMigration(ctx context.Context, path string, target int, backupPath string) (MigrationReport, error) {
	return applyMigration(ctx, path, target, backupPath, nil, false)
}

// ApplyMigrationWithLease performs the same exact-version migration while the
// caller retains an exclusive lease across a larger rollout transaction.
func ApplyMigrationWithLease(ctx context.Context, path string, target int, backupPath string, lease *Lease) (MigrationReport, error) {
	if lease == nil || lease.Mode() != LeaseExclusive {
		return MigrationReport{}, fmt.Errorf("store migration requires an exclusive maintenance lease")
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return MigrationReport{}, err
	}
	if lease.Path() != absPath {
		return MigrationReport{}, fmt.Errorf("maintenance lease protects %q, not %q", lease.Path(), absPath)
	}
	return applyMigration(ctx, absPath, target, backupPath, lease, false)
}

// PrepareMigrationBackupWithLease creates and verifies the exact rollback
// database before a generation rollout records its pre-mutation journal
// boundary. The returned digest is safe to bind into that journal.
func PrepareMigrationBackupWithLease(ctx context.Context, path string, target int, backupPath string, lease *Lease) (string, error) {
	if lease == nil || lease.Mode() != LeaseExclusive {
		return "", fmt.Errorf("migration backup requires an exclusive maintenance lease")
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if lease.Path() != absPath {
		return "", fmt.Errorf("maintenance lease protects %q, not %q", lease.Path(), absPath)
	}
	plan, err := PlanMigration(absPath, target)
	if err != nil {
		return "", err
	}
	if !plan.RequiresBackup {
		return "", nil
	}
	if strings.TrimSpace(backupPath) == "" {
		return "", fmt.Errorf("--backup is required when preparing a store migration")
	}
	backupPath = filepath.Clean(backupPath)
	if _, err := os.Stat(backupPath); err == nil {
		return "", fmt.Errorf("migration backup already exists: %s", backupPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := createPreMigrationBackup(ctx, plan.Path, backupPath); err != nil {
		return "", err
	}
	if err := verifyPreMigrationBackup(ctx, backupPath, plan); err != nil {
		return "", err
	}
	return sha256File(backupPath)
}

// ResumeMigrationWithLease permits reuse of the exact verified backup after
// an interrupted rollout. Ordinary apply still rejects pre-existing paths so
// an unrelated file can never be adopted accidentally.
func ResumeMigrationWithLease(ctx context.Context, path string, target int, backupPath string, lease *Lease) (MigrationReport, error) {
	if lease == nil || lease.Mode() != LeaseExclusive {
		return MigrationReport{}, fmt.Errorf("store migration recovery requires an exclusive maintenance lease")
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return MigrationReport{}, err
	}
	if lease.Path() != absPath {
		return MigrationReport{}, fmt.Errorf("maintenance lease protects %q, not %q", lease.Path(), absPath)
	}
	return applyMigration(ctx, absPath, target, backupPath, lease, true)
}

func applyMigration(ctx context.Context, path string, target int, backupPath string, heldLease *Lease, allowExistingBackup bool) (MigrationReport, error) {
	if err := ctx.Err(); err != nil {
		return MigrationReport{}, err
	}
	plan, err := PlanMigration(path, target)
	if err != nil {
		return MigrationReport{}, err
	}
	if !plan.RequiresBackup {
		return MigrationReport{MigrationPlan: plan, Status: "not-needed", ToStoreID: plan.FromStoreID}, nil
	}
	if strings.TrimSpace(backupPath) == "" {
		return MigrationReport{}, fmt.Errorf("--backup is required when applying a store migration")
	}
	backupPath = filepath.Clean(backupPath)
	backupExists := false
	if _, err := os.Stat(backupPath); err == nil {
		if !allowExistingBackup {
			return MigrationReport{}, fmt.Errorf("migration backup already exists: %s", backupPath)
		}
		backupExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return MigrationReport{}, err
	}
	lease := heldLease
	if lease == nil {
		lease, err = AcquireExclusiveLease(plan.Path)
		if err != nil {
			return MigrationReport{}, err
		}
		defer lease.Close()
	}
	// Re-plan under the exclusive lease so a stale plan cannot be applied.
	plan, err = PlanMigration(plan.Path, target)
	if err != nil {
		return MigrationReport{}, err
	}
	if !plan.RequiresBackup {
		return MigrationReport{MigrationPlan: plan, Status: "not-needed", ToStoreID: plan.FromStoreID}, nil
	}
	if backupExists {
		if err := verifyPreMigrationBackup(ctx, backupPath, plan); err != nil {
			return MigrationReport{}, fmt.Errorf("verify existing migration backup: %w", err)
		}
	} else {
		if err := createPreMigrationBackup(ctx, plan.Path, backupPath); err != nil {
			return MigrationReport{}, err
		}
	}
	backupDigest, err := sha256File(backupPath)
	if err != nil {
		return MigrationReport{}, fmt.Errorf("digest pre-migration backup: %w", err)
	}

	source, err := readMigrationSource(plan.Path)
	if err != nil {
		return MigrationReport{}, err
	}
	db, err := sql.Open("sqlite", plan.Path+"?_txlock=immediate")
	if err != nil {
		return MigrationReport{}, err
	}
	defer db.Close()
	if err := verifyPreMigrationHashes(ctx, db); err != nil {
		return MigrationReport{}, fmt.Errorf("pre-migration integrity verification failed: %w", err)
	}
	if err := configureDB(db); err != nil {
		return MigrationReport{}, err
	}
	if err := migrate(db); err != nil {
		return MigrationReport{}, err
	}

	var toStoreID, receiptID, receiptDigest string
	if source.storeID == "" {
		if err := ensureStoreIdentityAndHashes(db); err != nil {
			return MigrationReport{}, err
		}
		if err := db.QueryRow(`SELECT store_id FROM store_meta WHERE singleton = 1`).Scan(&toStoreID); err != nil {
			return MigrationReport{}, err
		}
	} else if source.format < 4 {
		toStoreID, receiptID, receiptDigest, err = migrateIdentityV1(db, source, backupDigest)
		if err != nil {
			return MigrationReport{}, err
		}
	} else {
		toStoreID, receiptID, receiptDigest, err = migrateFormatCurrent(ctx, db, source, backupDigest)
		if err != nil {
			return MigrationReport{}, err
		}
	}
	if err := ensureDerivedFresh(db); err != nil {
		return MigrationReport{}, err
	}
	if err := db.Close(); err != nil {
		return MigrationReport{}, err
	}
	if revision, err := inspectStoreFormat(plan.Path); err != nil || revision != target {
		return MigrationReport{}, fmt.Errorf("post-migration verification failed: revision=%d err=%v", revision, err)
	}
	return MigrationReport{MigrationPlan: plan, Status: "migrated", ToStoreID: toStoreID, ReceiptID: receiptID, ReceiptDigest: receiptDigest, BackupPath: backupPath}, nil
}

type migrationSource struct {
	storeID, head string
	eventCount    int64
	format        int
}

func readMigrationSource(path string) (migrationSource, error) {
	revision, err := inspectStoreFormat(path)
	if err != nil {
		return migrationSource{}, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return migrationSource{}, err
	}
	defer db.Close()
	var source migrationSource
	source.format = revision
	if err := db.QueryRow(`SELECT store_id, head_hash FROM store_meta WHERE singleton = 1`).Scan(&source.storeID, &source.head); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return migrationSource{}, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&source.eventCount); err != nil {
		return migrationSource{}, err
	}
	return source, nil
}

func readCurrentIdentityReadOnly(path string) (IdentityInfo, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return IdentityInfo{}, err
	}
	defer db.Close()
	var info IdentityInfo
	err = db.QueryRow(`SELECT store_id, identity_scheme, document_bytes, document_digest, artifact_namespace FROM store_identity_v1 WHERE singleton=1`).Scan(
		&info.StoreID, &info.Scheme, &info.DocumentBytes, &info.DocumentDigest, &info.ArtifactNamespace,
	)
	return info, err
}

func createPreMigrationBackup(ctx context.Context, source, destination string) error {
	if dir := filepath.Dir(destination); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		return err
	}
	defer db.Close()
	quoted := strings.ReplaceAll(destination, "'", "''")
	if _, err := db.ExecContext(ctx, `VACUUM INTO '`+quoted+`'`); err != nil {
		return fmt.Errorf("create pre-migration backup: %w", err)
	}
	if info, err := os.Stat(destination); err != nil || info.Size() == 0 {
		return fmt.Errorf("verify pre-migration backup: size=%d err=%v", func() int64 {
			if info == nil {
				return 0
			}
			return info.Size()
		}(), err)
	}
	return nil
}

func verifyPreMigrationBackup(ctx context.Context, backupPath string, plan MigrationPlan) error {
	backup, err := readMigrationSource(backupPath)
	if err != nil {
		return err
	}
	if backup.format != plan.FromFormat || backup.storeID != plan.FromStoreID {
		return fmt.Errorf("backup generation mismatch: format=%d store_id=%q, expected format=%d store_id=%q",
			backup.format, backup.storeID, plan.FromFormat, plan.FromStoreID)
	}
	current, err := readMigrationSource(plan.Path)
	if err != nil {
		return err
	}
	if backup.head != current.head || backup.eventCount != current.eventCount {
		return fmt.Errorf("backup ledger mismatch: head=%q events=%d, current head=%q events=%d",
			backup.head, backup.eventCount, current.head, current.eventCount)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Clean(backupPath))+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	if err := verifyPreMigrationHashes(ctx, db); err != nil {
		return fmt.Errorf("backup integrity verification failed: %w", err)
	}
	return nil
}

func verifyPreMigrationHashes(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyHashesTx(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateIdentityV1(db *sql.DB, source migrationSource, backupDigest string) (string, string, string, error) {
	document, err := storeidentity.NewDocumentV1()
	if err != nil {
		return "", "", "", err
	}
	documentBytes := document.CanonicalBytes()
	toStoreID := document.StoreID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := identityMigrationReceiptV1{
		Version: "store-identity-migration-v1", FromStoreID: source.storeID, FromIdentityScheme: "missis-ulid-v1",
		ToStoreID: toStoreID, ToIdentityScheme: storeidentity.Scheme, SourceHeadDigest: source.head,
		SourceHeadIntegrityEpoch: "global-json-chain-v1", SourceEventCount: source.eventCount,
		SourceFormatRevision: source.format, TargetFormatRevision: CurrentStoreFormatRevision,
		ArtifactNamespace: source.storeID, BackupDatabaseSHA256: backupDigest, MigratedAt: now,
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return "", "", "", err
	}
	receiptSum := sha256.Sum256(receiptBytes)
	receiptDigest := "sha256:" + hex.EncodeToString(receiptSum[:])
	receiptID := "identity-migration:" + hex.EncodeToString(receiptSum[:])
	tx, err := db.Begin()
	if err != nil {
		return "", "", "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO store_identity_v1(singleton,store_id,identity_scheme,document_bytes,document_digest,artifact_namespace,created_at,creator_protocol,creator_contract_digest) VALUES(1,?,?,?,?,?,?,?,NULL)`,
		toStoreID, storeidentity.Scheme, documentBytes, storeidentity.DocumentDigest(documentBytes), source.storeID, now, "eventstore-v3-alpha.3"); err != nil {
		return "", "", "", err
	}
	if _, err := tx.Exec(`INSERT INTO store_identity_migration_receipts(receipt_id,from_store_id,from_identity_scheme,to_store_id,to_identity_scheme,source_head_digest,source_head_integrity_epoch,source_event_count,source_format_revision,target_format_revision,artifact_namespace,backup_database_sha256,migrated_at,receipt_bytes,receipt_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		receiptID, source.storeID, "missis-ulid-v1", toStoreID, storeidentity.Scheme, source.head, "global-json-chain-v1", source.eventCount, source.format, CurrentStoreFormatRevision, source.storeID, backupDigest, now, receiptBytes, receiptDigest); err != nil {
		return "", "", "", err
	}
	if _, err := tx.Exec(`UPDATE store_meta SET store_id=?, format_revision=?, updated_at=? WHERE singleton=1`, toStoreID, CurrentStoreFormatRevision, now); err != nil {
		return "", "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", "", err
	}
	return toStoreID, receiptID, receiptDigest, nil
}

func migrateFormatCurrent(ctx context.Context, db *sql.DB, source migrationSource, backupDigest string) (string, string, string, error) {
	if source.format < 4 || source.format >= CurrentStoreFormatRevision {
		return "", "", "", fmt.Errorf("format-only migration requires source format 4-%d, found %d", CurrentStoreFormatRevision-1, source.format)
	}
	var info IdentityInfo
	if err := db.QueryRowContext(ctx, `SELECT store_id,identity_scheme,document_bytes,document_digest,artifact_namespace FROM store_identity_v1 WHERE singleton=1`).Scan(
		&info.StoreID, &info.Scheme, &info.DocumentBytes, &info.DocumentDigest, &info.ArtifactNamespace,
	); err != nil {
		return "", "", "", err
	}
	if err := validateIdentityInfo(info); err != nil {
		return "", "", "", err
	}
	if info.StoreID != source.storeID {
		return "", "", "", fmt.Errorf("format migration identity changed: source=%q current=%q", source.storeID, info.StoreID)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := formatMigrationReceiptV1{
		Version: "store-format-migration-v1", StoreID: info.StoreID,
		SourceHeadDigest: source.head, SourceHeadIntegrityEpoch: "global-json-chain-v1",
		SourceEventCount: source.eventCount, SourceFormatRevision: source.format,
		TargetFormatRevision: CurrentStoreFormatRevision,
		BackupDatabaseSHA256: backupDigest, MigratedAt: now,
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return "", "", "", err
	}
	receiptSum := sha256.Sum256(receiptBytes)
	receiptHex := hex.EncodeToString(receiptSum[:])
	receiptDigest := "sha256:" + receiptHex
	receiptID := "format-migration:" + receiptHex
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO store_format_migration_receipts(
		receipt_id,source_format_revision,target_format_revision,store_id,
		source_head_digest,source_head_integrity_epoch,source_event_count,
		backup_database_sha256,migrated_at,receipt_bytes,receipt_digest
	) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, receiptID, source.format, CurrentStoreFormatRevision,
		info.StoreID, source.head, "global-json-chain-v1", source.eventCount,
		backupDigest, now, receiptBytes, receiptDigest); err != nil {
		return "", "", "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_meta SET format_revision=?,updated_at=? WHERE singleton=1`, CurrentStoreFormatRevision, now); err != nil {
		return "", "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", "", err
	}
	return info.StoreID, receiptID, receiptDigest, nil
}

func declareWritableForkV1(ctx context.Context, db *sql.DB, plan WritableForkPlan, backupDigest string) (string, string, string, error) {
	var from IdentityInfo
	var head string
	var count int64
	if err := db.QueryRowContext(ctx, `SELECT store_id,identity_scheme,document_bytes,document_digest,artifact_namespace FROM store_identity_v1 WHERE singleton=1`).Scan(
		&from.StoreID, &from.Scheme, &from.DocumentBytes, &from.DocumentDigest, &from.ArtifactNamespace,
	); err != nil {
		return "", "", "", err
	}
	if err := validateIdentityInfo(from); err != nil {
		return "", "", "", err
	}
	if from.StoreID != plan.FromStoreID {
		return "", "", "", fmt.Errorf("fork source identity changed: planned %q, found %q", plan.FromStoreID, from.StoreID)
	}
	if err := db.QueryRowContext(ctx, `SELECT head_hash FROM store_meta WHERE singleton=1`).Scan(&head); err != nil {
		return "", "", "", err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return "", "", "", err
	}
	document, err := storeidentity.NewDocumentV1()
	if err != nil {
		return "", "", "", err
	}
	documentBytes := document.CanonicalBytes()
	toStoreID := document.StoreID()
	toDocumentDigest := storeidentity.DocumentDigest(documentBytes)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := writableForkReceiptV1{
		Version: "store-identity-fork-v1", FromStoreID: from.StoreID,
		FromIdentityScheme: from.Scheme, FromIdentityDocument: from.DocumentBytes,
		FromIdentityDocumentDigest: from.DocumentDigest,
		FromHeadDigest:             head, FromHeadIntegrityEpoch: "global-json-chain-v1",
		FromEventCount: count, FromFormatRevision: plan.FormatRevision,
		ToStoreID: toStoreID, ToIdentityScheme: storeidentity.Scheme,
		ToIdentityDocumentDigest: toDocumentDigest, ArtifactDisposition: "new-empty-namespace",
		ArtifactNamespace: toStoreID, BackupDatabaseSHA256: backupDigest, ForkedAt: now,
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return "", "", "", err
	}
	receiptSum := sha256.Sum256(receiptBytes)
	receiptHex := hex.EncodeToString(receiptSum[:])
	receiptDigest := "sha256:" + receiptHex
	receiptID := "identity-fork:" + receiptHex
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO store_identity_migration_receipts(
		receipt_id,from_store_id,from_identity_scheme,to_store_id,to_identity_scheme,
		source_head_digest,source_head_integrity_epoch,source_event_count,source_format_revision,
		target_format_revision,artifact_namespace,backup_database_sha256,migrated_at,receipt_bytes,receipt_digest
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		receiptID, from.StoreID, from.Scheme, toStoreID, storeidentity.Scheme,
		head, "global-json-chain-v1", count, plan.FormatRevision, plan.FormatRevision,
		toStoreID, backupDigest, now, receiptBytes, receiptDigest,
	); err != nil {
		return "", "", "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_identity_v1 SET
		store_id=?,identity_scheme=?,document_bytes=?,document_digest=?,artifact_namespace=?,
		created_at=?,creator_protocol=?,creator_contract_digest=NULL WHERE singleton=1`,
		toStoreID, storeidentity.Scheme, documentBytes, toDocumentDigest, toStoreID,
		now, "eventstore-v3-alpha.3"); err != nil {
		return "", "", "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_meta SET store_id=?,updated_at=? WHERE singleton=1`, toStoreID, now); err != nil {
		return "", "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", "", err
	}
	return toStoreID, receiptID, receiptDigest, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
