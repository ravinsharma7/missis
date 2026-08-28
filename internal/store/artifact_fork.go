package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/fsutil"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/storeidentity"
)

const (
	artifactForkOperationFilename = "artifact-namespace-fork-operation-v1.json"
	artifactForkManifestFilename  = "artifact-namespace-fork-manifest-v1.json"
	artifactForkCompleteFilename  = "artifact-namespace-fork-complete-v1.json"
)

func validateArtifactForkRoots(sourceRoot, destinationRoot string, requireSource bool) error {
	sourceRoot = strings.TrimSpace(sourceRoot)
	destinationRoot = strings.TrimSpace(destinationRoot)
	if requireSource && sourceRoot == "" {
		return errors.New("source artifact root is required")
	}
	if destinationRoot == "" {
		return errors.New("destination artifact root is required")
	}
	destinationAbs, err := filepath.Abs(filepath.Clean(destinationRoot))
	if err != nil {
		return err
	}
	for _, candidate := range []string{destinationAbs, destinationAbs + ".artifact-namespace-fork-v1.staging"} {
		if info, statErr := os.Lstat(candidate); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact fork root must not be a symlink: %s", candidate)
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	if sourceRoot == "" {
		return nil
	}
	sourceAbs, err := filepath.Abs(filepath.Clean(sourceRoot))
	if err != nil {
		return err
	}
	if info, err := os.Lstat(sourceAbs); err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("artifact fork root must not be a symlink: %s", sourceAbs)
	}
	if sourceAbs == destinationAbs || pathContains(sourceAbs, destinationAbs) || pathContains(destinationAbs, sourceAbs) {
		return errors.New("source and destination artifact roots must be distinct and non-nested")
	}
	return nil
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// WritableForkOptions supplies the two exact local artifact namespaces used
// by a fork. Paths are operational configuration and are deliberately absent
// from accepted events, store identity, manifests, and receipts.
type WritableForkOptions struct {
	SourceArtifactRoot      string
	DestinationArtifactRoot string
	ExecutionMode           WritableForkExecutionMode
	// Fault is test-only deterministic injection. Production callers leave it
	// nil. A returned error stops at the named durable boundary.
	Fault func(stage string) error
}

type WritableForkExecutionMode string

const (
	WritableForkApplyV1   WritableForkExecutionMode = "artifact-namespace-fork-apply-v1"
	WritableForkRecoverV1 WritableForkExecutionMode = "artifact-namespace-fork-recover-v1"
)

type artifactForkInventory struct {
	Records   map[string]ArtifactRecord
	Managed   map[string]ArtifactReferenceUsage
	Unmanaged []ArtifactReferenceUsage
}

type artifactForkOperationV1 struct {
	Version                    string `json:"version"`
	FromStoreID                string `json:"from_store_id"`
	FromIdentityDocumentDigest string `json:"from_identity_document_digest"`
	FromHeadDigest             string `json:"from_head_digest"`
	FromHeadIntegrityEpoch     string `json:"from_head_integrity_epoch"`
	FromEventCount             int64  `json:"from_event_count"`
	ToStoreID                  string `json:"to_store_id"`
	ToIdentityScheme           string `json:"to_identity_scheme"`
	ToIdentityDocument         []byte `json:"to_identity_document"`
	ToIdentityDocumentDigest   string `json:"to_identity_document_digest"`
	ArtifactForkProtocol       string `json:"artifact_fork_protocol"`
	ReceiptVersion             string `json:"receipt_version"`
	BackupDatabaseSHA256       string `json:"backup_database_sha256"`
	CreatedAt                  string `json:"created_at"`
}

type artifactForkManifestEntryV1 struct {
	Ref       string   `json:"ref"`
	Algorithm string   `json:"algorithm"`
	Digest    string   `json:"digest"`
	MediaType string   `json:"media_type,omitempty"`
	Size      int64    `json:"size"`
	Indexed   bool     `json:"indexed"`
	Accepted  bool     `json:"accepted"`
	Backend   string   `json:"backend,omitempty"`
	EventIDs  []string `json:"event_ids,omitempty"`
	Locations []string `json:"locations,omitempty"`
}

type artifactForkManifestV1 struct {
	Version                      string                        `json:"version"`
	FromStoreID                  string                        `json:"from_store_id"`
	ToStoreID                    string                        `json:"to_store_id"`
	SourceHeadDigest             string                        `json:"source_head_digest"`
	SourceEventCount             int64                         `json:"source_event_count"`
	OperationDigest              string                        `json:"operation_digest"`
	BackupDatabaseSHA256         string                        `json:"backup_database_sha256"`
	Objects                      []artifactForkManifestEntryV1 `json:"objects"`
	UnmanagedReferences          []ArtifactReferenceUsage      `json:"unmanaged_references"`
	ExcludedUnreferencedObjects  []string                      `json:"excluded_unreferenced_objects"`
	CopiedObjectCount            int                           `json:"copied_object_count"`
	CopiedByteCount              int64                         `json:"copied_byte_count"`
	UnmanagedReferenceCount      int                           `json:"unmanaged_reference_count"`
	ExcludedUnreferencedObjCount int                           `json:"excluded_unreferenced_object_count"`
}

type artifactForkCompleteV1 struct {
	Version              string `json:"version"`
	FromStoreID          string `json:"from_store_id"`
	ToStoreID            string `json:"to_store_id"`
	ManifestDigest       string `json:"manifest_digest"`
	OperationDigest      string `json:"operation_digest"`
	BackupDatabaseSHA256 string `json:"backup_database_sha256"`
	CompletedAt          string `json:"completed_at"`
}

type writableForkReceiptV2 struct {
	writableForkReceiptV1
	ArtifactManifestDigest       string `json:"artifact_manifest_digest"`
	CompletionMarkerDigest       string `json:"completion_marker_digest"`
	CopiedObjectCount            int    `json:"copied_object_count"`
	CopiedByteCount              int64  `json:"copied_byte_count"`
	UnmanagedReferenceCount      int    `json:"unmanaged_reference_count"`
	ExcludedUnreferencedObjCount int    `json:"excluded_unreferenced_object_count"`
	UnmanagedDisposition         string `json:"unmanaged_disposition"`
	ExcludedObjectDisposition    string `json:"excluded_object_disposition"`
	FromArtifactNamespace        string `json:"from_artifact_namespace"`
	ArtifactForkProtocol         string `json:"artifact_fork_protocol"`
}

type ArtifactForkInspection struct {
	Status                  string   `json:"status"`
	Root                    string   `json:"root"`
	FromStoreID             string   `json:"from_store_id,omitempty"`
	ToStoreID               string   `json:"to_store_id,omitempty"`
	DatabaseStoreID         string   `json:"database_store_id,omitempty"`
	ManifestDigest          string   `json:"manifest_digest,omitempty"`
	CompletionMarkerDigest  string   `json:"completion_marker_digest,omitempty"`
	CopiedObjectCount       int      `json:"copied_object_count"`
	CopiedByteCount         int64    `json:"copied_byte_count"`
	UnmanagedReferenceCount int      `json:"unmanaged_reference_count"`
	ExcludedObjectCount     int      `json:"excluded_object_count"`
	NamespacePublished      bool     `json:"namespace_published"`
	Issues                  []string `json:"issues,omitempty"`
}

// InspectArtifactNamespaceFork verifies a staged or published local namespace
// and correlates it with the database identity. It never creates or repairs a
// path. A healthy parent identity means the namespace is prepared but its
// database commit has not happened; a healthy child identity means complete.
func InspectArtifactNamespaceFork(ctx context.Context, databasePath, artifactRoot string) (ArtifactForkInspection, error) {
	if err := ctx.Err(); err != nil {
		return ArtifactForkInspection{}, err
	}
	root, err := filepath.Abs(filepath.Clean(strings.TrimSpace(artifactRoot)))
	if err != nil || strings.TrimSpace(artifactRoot) == "" {
		return ArtifactForkInspection{}, errors.New("artifact fork root is required")
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		staging := root + ".artifact-namespace-fork-v1.staging"
		if _, stagingErr := os.Stat(staging); stagingErr != nil {
			if errors.Is(stagingErr, os.ErrNotExist) {
				return ArtifactForkInspection{Status: "absent", Root: root}, nil
			}
			return ArtifactForkInspection{}, stagingErr
		}
		root = staging
	} else if err != nil {
		return ArtifactForkInspection{}, err
	}
	report := ArtifactForkInspection{Root: root, NamespacePublished: !strings.HasSuffix(root, ".artifact-namespace-fork-v1.staging")}
	operationBytes, err := os.ReadFile(filepath.Join(root, artifactForkOperationFilename))
	if errors.Is(err, os.ErrNotExist) {
		report.Status = "incomplete-without-operation"
		report.Issues = append(report.Issues, "missing-operation-record")
		return report, nil
	}
	if err != nil {
		return report, err
	}
	var operation artifactForkOperationV1
	if err := json.Unmarshal(operationBytes, &operation); err != nil {
		return report, fmt.Errorf("decode artifact fork operation: %w", err)
	}
	report.FromStoreID, report.ToStoreID = operation.FromStoreID, operation.ToStoreID
	operationDigest := digestBytes(operationBytes)
	if operation.Version != "artifact-namespace-fork-operation-v1" || storeidentity.ValidateBinding(operation.ToStoreID, operation.ToIdentityDocument) != nil ||
		storeidentity.DocumentDigest(operation.ToIdentityDocument) != operation.ToIdentityDocumentDigest {
		report.Status = "invalid"
		report.Issues = append(report.Issues, "invalid-operation-record")
		return report, nil
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, artifactForkManifestFilename))
	if errors.Is(err, os.ErrNotExist) {
		report.Status = "copy-incomplete"
		return correlateArtifactForkDatabase(databasePath, report)
	}
	if err != nil {
		return report, err
	}
	var manifest artifactForkManifestV1
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return report, fmt.Errorf("decode artifact fork manifest: %w", err)
	}
	report.ManifestDigest = digestBytes(manifestBytes)
	report.CopiedObjectCount, report.CopiedByteCount = manifest.CopiedObjectCount, manifest.CopiedByteCount
	report.UnmanagedReferenceCount, report.ExcludedObjectCount = manifest.UnmanagedReferenceCount, manifest.ExcludedUnreferencedObjCount
	if manifest.Version != "artifact-namespace-fork-manifest-v1" || manifest.FromStoreID != operation.FromStoreID || manifest.ToStoreID != operation.ToStoreID ||
		manifest.OperationDigest != operationDigest || manifest.BackupDatabaseSHA256 != operation.BackupDatabaseSHA256 || len(manifest.Objects) != manifest.CopiedObjectCount {
		report.Issues = append(report.Issues, "manifest-fields-mismatch")
	}
	local, err := artifact.OpenLocalStore(root)
	if err != nil {
		return report, err
	}
	var computedBytes int64
	for _, entry := range manifest.Objects {
		ref, parseErr := artifact.ParseRef(entry.Ref)
		if parseErr != nil {
			report.Issues = append(report.Issues, "invalid-manifest-ref:"+entry.Ref)
			continue
		}
		metadata, verifyErr := local.Verify(ctx, ref)
		if verifyErr != nil {
			report.Issues = append(report.Issues, "missing-or-corrupt-object:"+entry.Ref)
			continue
		}
		if metadata.Algorithm != entry.Algorithm || metadata.Digest != entry.Digest || metadata.Size != entry.Size || metadata.MediaType != entry.MediaType {
			report.Issues = append(report.Issues, "object-metadata-mismatch:"+entry.Ref)
		}
		computedBytes += metadata.Size
	}
	if computedBytes != manifest.CopiedByteCount {
		report.Issues = append(report.Issues, "copied-byte-count-mismatch")
	}
	completeBytes, err := os.ReadFile(filepath.Join(root, artifactForkCompleteFilename))
	if errors.Is(err, os.ErrNotExist) {
		report.Status = "manifest-written-copy-incomplete"
		return correlateArtifactForkDatabase(databasePath, report)
	}
	if err != nil {
		return report, err
	}
	report.CompletionMarkerDigest = digestBytes(completeBytes)
	var complete artifactForkCompleteV1
	if err := json.Unmarshal(completeBytes, &complete); err != nil {
		return report, fmt.Errorf("decode artifact fork completion marker: %w", err)
	}
	if complete.Version != "artifact-namespace-fork-complete-v1" || complete.FromStoreID != operation.FromStoreID || complete.ToStoreID != operation.ToStoreID ||
		complete.ManifestDigest != report.ManifestDigest || complete.OperationDigest != operationDigest || complete.BackupDatabaseSHA256 != operation.BackupDatabaseSHA256 {
		report.Issues = append(report.Issues, "completion-marker-mismatch")
	}
	if len(report.Issues) > 0 {
		report.Status = "integrity-failure"
		return correlateArtifactForkDatabase(databasePath, report)
	}
	report.Status = "namespace-published"
	return correlateArtifactForkDatabase(databasePath, report)
}

func correlateArtifactForkDatabase(databasePath string, report ArtifactForkInspection) (ArtifactForkInspection, error) {
	info, err := readCurrentIdentityReadOnly(filepath.Clean(databasePath))
	if err != nil {
		return report, err
	}
	report.DatabaseStoreID = info.StoreID
	if len(report.Issues) > 0 {
		return report, nil
	}
	switch info.StoreID {
	case report.FromStoreID:
		if report.Status == "namespace-published" {
			if report.NamespacePublished {
				report.Status = "prepared-awaiting-database-commit"
			} else {
				report.Status = "prepared-awaiting-namespace-publication"
			}
		}
	case report.ToStoreID:
		if report.Status != "namespace-published" {
			report.Status = "integrity-failure"
			report.Issues = append(report.Issues, "database-committed-with-incomplete-namespace")
		} else {
			db, openErr := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Clean(databasePath))+"?mode=ro")
			if openErr != nil {
				return report, openErr
			}
			var manifestDigest, markerDigest string
			queryErr := db.QueryRow(`SELECT manifest_digest,completion_marker_digest FROM artifact_namespace_forks WHERE to_store_id=? ORDER BY completed_at DESC LIMIT 1`, report.ToStoreID).Scan(&manifestDigest, &markerDigest)
			closeErr := db.Close()
			if queryErr != nil || manifestDigest != report.ManifestDigest || markerDigest != report.CompletionMarkerDigest {
				report.Status = "integrity-failure"
				report.Issues = append(report.Issues, "database-receipt-does-not-match-namespace")
			} else if closeErr != nil {
				return report, closeErr
			} else {
				report.Status = "complete"
			}
		}
	default:
		report.Status = "identity-mismatch"
		report.Issues = append(report.Issues, "database-identity-matches-neither-parent-nor-child")
	}
	return report, nil
}

func loadArtifactForkInventory(ctx context.Context, db *sql.DB) (artifactForkInventory, error) {
	result := artifactForkInventory{Records: map[string]ArtifactRecord{}, Managed: map[string]ArtifactReferenceUsage{}}
	rows, err := db.QueryContext(ctx, `SELECT ref,algorithm,digest,media_type,size,backend,recorded_at FROM artifacts ORDER BY ref`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var record ArtifactRecord
		if err := scanArtifactRecord(rows, &record); err != nil {
			rows.Close()
			return result, err
		}
		result.Records[record.Ref] = record
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	eventRows, err := db.QueryContext(ctx, `SELECT event_json FROM events ORDER BY alias_seq`)
	if err != nil {
		return result, err
	}
	defer eventRows.Close()
	unmanaged := map[string]*ArtifactReferenceUsage{}
	for eventRows.Next() {
		var raw []byte
		if err := eventRows.Scan(&raw); err != nil {
			return result, err
		}
		var event model.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return result, fmt.Errorf("decode accepted event while preparing artifact fork: %w", err)
		}
		occurrences, err := model.AcceptedArtifactReferences(event)
		if err != nil {
			return result, err
		}
		for _, occurrence := range occurrences {
			if occurrence.Managed {
				usage := result.Managed[occurrence.Ref]
				usage.Ref, usage.Managed = occurrence.Ref, true
				usage.EventIDs = append(usage.EventIDs, string(occurrence.EventID))
				usage.Locations = append(usage.Locations, occurrence.Location)
				result.Managed[occurrence.Ref] = usage
				continue
			}
			usage := unmanaged[occurrence.Ref]
			if usage == nil {
				usage = &ArtifactReferenceUsage{Ref: occurrence.Ref}
				unmanaged[occurrence.Ref] = usage
			}
			usage.EventIDs = append(usage.EventIDs, string(occurrence.EventID))
			usage.Locations = append(usage.Locations, occurrence.Location)
		}
	}
	if err := eventRows.Err(); err != nil {
		return result, err
	}
	keys := make([]string, 0, len(unmanaged))
	for ref := range unmanaged {
		keys = append(keys, ref)
	}
	sort.Strings(keys)
	for _, ref := range keys {
		result.Unmanaged = append(result.Unmanaged, *unmanaged[ref])
	}
	return result, nil
}

// InspectWritableForkArtifactInventory adds a full read-only CAS inventory to
// a database-only fork plan. It creates no root, lock, child identity, backup,
// or staging state.
func InspectWritableForkArtifactInventory(ctx context.Context, plan WritableForkPlan, sourceRoot string) (WritableForkPlan, error) {
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if !plan.RequiresArtifactNamespaceFork || (plan.ArtifactRecordCount == 0 && plan.ManagedCASReferenceOccurrences == 0) {
		plan.ArtifactInventoryStatus = "not-required"
		return plan, nil
	}
	root, err := filepath.Abs(filepath.Clean(strings.TrimSpace(sourceRoot)))
	if err != nil || strings.TrimSpace(sourceRoot) == "" {
		return plan, errors.New("source artifact root is required for full fork inventory")
	}
	if info, err := os.Lstat(root); err != nil {
		return plan, err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return plan, fmt.Errorf("artifact fork root must not be a symlink: %s", root)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Clean(plan.Path))+"?mode=ro")
	if err != nil {
		return plan, err
	}
	defer db.Close()
	inventory, err := loadArtifactForkInventory(ctx, db)
	if err != nil {
		return plan, err
	}
	required := make(map[string]struct{}, len(inventory.Records)+len(inventory.Managed))
	for ref := range inventory.Records {
		required[ref] = struct{}{}
	}
	for ref := range inventory.Managed {
		required[ref] = struct{}{}
	}
	plan.RequiredManagedObjectCount = len(required)
	local, err := artifact.OpenLocalStore(root)
	if err != nil {
		return plan, err
	}
	objects, err := local.Scan(ctx)
	if err != nil {
		return plan, err
	}
	present := map[string]artifact.Object{}
	for _, object := range objects {
		if object.Ref == "" || !object.Valid {
			plan.ArtifactIntegrityIssues = append(plan.ArtifactIntegrityIssues, fmt.Sprintf("invalid-source-object:%v", object.Err))
			continue
		}
		present[object.Ref.String()] = object
		if _, needed := required[object.Ref.String()]; !needed {
			plan.ExcludedSourceObjectRefs = append(plan.ExcludedSourceObjectRefs, object.Ref.String())
		}
	}
	for ref := range required {
		if _, ok := present[ref]; !ok {
			plan.ArtifactIntegrityIssues = append(plan.ArtifactIntegrityIssues, "missing-or-corrupt-required-object:"+ref)
		}
	}
	sort.Strings(plan.ExcludedSourceObjectRefs)
	sort.Strings(plan.ArtifactIntegrityIssues)
	plan.ExcludedSourceObjectCount = len(plan.ExcludedSourceObjectRefs)
	if len(plan.ArtifactIntegrityIssues) > 0 {
		plan.ArtifactInventoryStatus = "integrity-failure"
		plan.Eligible = false
		if plan.BlockedReason == "" {
			plan.BlockedReason = "source artifact inventory contains missing, corrupt, or invalid objects"
		}
	} else {
		plan.ArtifactInventoryStatus = "verified"
	}
	return plan, nil
}

func prepareArtifactNamespaceFork(ctx context.Context, db *sql.DB, plan WritableForkPlan, backupDigest string, options WritableForkOptions) (artifactForkOperationV1, artifactForkManifestV1, artifactForkCompleteV1, string, string, error) {
	inventory, err := loadArtifactForkInventory(ctx, db)
	if err != nil {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
	}
	required := make(map[string]struct{}, len(inventory.Records)+len(inventory.Managed))
	for ref := range inventory.Records {
		required[ref] = struct{}{}
	}
	for ref := range inventory.Managed {
		required[ref] = struct{}{}
	}
	if len(required) == 0 && len(inventory.Unmanaged) == 0 {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", errors.New("artifact namespace preparation requested for an empty inventory")
	}
	sourceRoot := strings.TrimSpace(options.SourceArtifactRoot)
	destinationRoot := strings.TrimSpace(options.DestinationArtifactRoot)
	if len(required) > 0 && sourceRoot == "" {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", errors.New("--source-artifact-root is required because managed or indexed artifacts must be copied")
	}
	if destinationRoot == "" {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", errors.New("--destination-artifact-root is required for an artifact namespace fork")
	}
	if sourceRoot != "" {
		sourceRoot, err = filepath.Abs(filepath.Clean(sourceRoot))
		if err != nil {
			return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
		}
	}
	destinationRoot, err = filepath.Abs(filepath.Clean(destinationRoot))
	if err != nil {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
	}
	if sourceRoot != "" && sourceRoot == destinationRoot {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", errors.New("source and destination artifact roots must differ; a writable fork requires independent bytes")
	}

	var from IdentityInfo
	var head, integrityEpoch string
	var eventCount int64
	if err := db.QueryRowContext(ctx, `SELECT store_id,identity_scheme,document_bytes,document_digest,artifact_namespace FROM store_identity_v1 WHERE singleton=1`).Scan(
		&from.StoreID, &from.Scheme, &from.DocumentBytes, &from.DocumentDigest, &from.ArtifactNamespace,
	); err != nil {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
	}
	if err := db.QueryRowContext(ctx, `SELECT head_hash,integrity_epoch FROM store_meta WHERE singleton=1`).Scan(&head, &integrityEpoch); err != nil {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
	}
	if from.StoreID != plan.FromStoreID || eventCount != plan.EventCount {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", errors.New("fork source changed after planning")
	}

	stageRoot := destinationRoot + ".artifact-namespace-fork-v1.staging"
	operationPath := filepath.Join(stageRoot, artifactForkOperationFilename)
	destinationAlreadyExists := false
	if _, statErr := os.Stat(destinationRoot); statErr == nil {
		destinationAlreadyExists = true
		operationPath = filepath.Join(destinationRoot, artifactForkOperationFilename)
		stageRoot = destinationRoot
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return artifactForkOperationV1{}, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", statErr
	}
	var operation artifactForkOperationV1
	operationBytes, readErr := os.ReadFile(operationPath)
	if readErr == nil {
		if err := json.Unmarshal(operationBytes, &operation); err != nil {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", fmt.Errorf("decode artifact fork operation: %w", err)
		}
		if err := validateArtifactForkOperation(operation, from, head, integrityEpoch, eventCount, backupDigest); err != nil {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
		}
		if destinationAlreadyExists {
			inspection, inspectErr := InspectArtifactNamespaceFork(ctx, plan.Path, destinationRoot)
			if inspectErr != nil || inspection.Status != "prepared-awaiting-database-commit" {
				return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", fmt.Errorf("existing destination is not an exact prepared namespace: status=%q issues=%v err=%v", inspection.Status, inspection.Issues, inspectErr)
			}
			manifestBytes, err := os.ReadFile(filepath.Join(destinationRoot, artifactForkManifestFilename))
			if err != nil {
				return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
			}
			var manifest artifactForkManifestV1
			if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
				return operation, manifest, artifactForkCompleteV1{}, "", "", err
			}
			completeBytes, err := os.ReadFile(filepath.Join(destinationRoot, artifactForkCompleteFilename))
			if err != nil {
				return operation, manifest, artifactForkCompleteV1{}, "", "", err
			}
			var complete artifactForkCompleteV1
			if err := json.Unmarshal(completeBytes, &complete); err != nil {
				return operation, manifest, complete, "", "", err
			}
			return operation, manifest, complete, digestBytes(manifestBytes), digestBytes(completeBytes), nil
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", readErr
	} else {
		if destinationAlreadyExists {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", fmt.Errorf("destination artifact root already exists without a matching fork operation record: %s", destinationRoot)
		}
		if _, stageErr := os.Stat(stageRoot); stageErr == nil {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", fmt.Errorf("incomplete staging exists without an operation record: %s; inspect and remove it explicitly before retrying", stageRoot)
		} else if !errors.Is(stageErr, os.ErrNotExist) {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", stageErr
		}
		document, err := storeidentity.NewDocumentV1()
		if err != nil {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
		}
		documentBytes := document.CanonicalBytes()
		operation = artifactForkOperationV1{
			Version: "artifact-namespace-fork-operation-v1", FromStoreID: from.StoreID,
			FromIdentityDocumentDigest: from.DocumentDigest, FromHeadDigest: head, FromHeadIntegrityEpoch: integrityEpoch, FromEventCount: eventCount,
			ToStoreID: document.StoreID(), ToIdentityScheme: storeidentity.Scheme,
			ToIdentityDocument: documentBytes, ToIdentityDocumentDigest: storeidentity.DocumentDigest(documentBytes),
			ArtifactForkProtocol: "artifact-namespace-fork-v1", ReceiptVersion: "store-identity-fork-v2",
			BackupDatabaseSHA256: backupDigest,
			CreatedAt:            time.Now().UTC().Format(time.RFC3339Nano),
		}
		if _, err := artifact.NewLocalStore(stageRoot); err != nil {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
		}
		operationBytes, _ = json.Marshal(operation)
		if err := writeDurableFile(operationPath, operationBytes); err != nil {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
		}
	}

	var source *artifact.LocalStore
	if len(required) > 0 {
		source, err = artifact.OpenLocalStore(sourceRoot)
		if err != nil {
			return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", fmt.Errorf("open source artifact namespace: %w", err)
		}
	}
	destination, err := artifact.OpenLocalStore(stageRoot)
	if err != nil {
		return operation, artifactForkManifestV1{}, artifactForkCompleteV1{}, "", "", err
	}
	refs := make([]string, 0, len(required))
	for ref := range required {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	operationDigest := digestBytes(operationBytes)
	manifest := artifactForkManifestV1{
		Version: "artifact-namespace-fork-manifest-v1", FromStoreID: from.StoreID, ToStoreID: operation.ToStoreID,
		SourceHeadDigest: head, SourceEventCount: eventCount, UnmanagedReferences: inventory.Unmanaged,
		UnmanagedReferenceCount: len(inventory.Unmanaged), OperationDigest: operationDigest, BackupDatabaseSHA256: backupDigest,
	}
	for _, rawRef := range refs {
		ref, err := artifact.ParseRef(rawRef)
		if err != nil {
			return operation, manifest, artifactForkCompleteV1{}, "", "", err
		}
		metadata, err := source.Verify(ctx, ref)
		if err != nil {
			return operation, manifest, artifactForkCompleteV1{}, "", "", fmt.Errorf("artifact-integrity-failure: required %s is missing or corrupt: %w", rawRef, err)
		}
		if record, ok := inventory.Records[rawRef]; ok && (record.Algorithm != metadata.Algorithm || record.Digest != metadata.Digest || record.Size != metadata.Size) {
			return operation, manifest, artifactForkCompleteV1{}, "", "", fmt.Errorf("artifact-integrity-failure: index metadata does not match source object %s", rawRef)
		}
		reader, err := source.Open(ctx, ref)
		if err != nil {
			return operation, manifest, artifactForkCompleteV1{}, "", "", err
		}
		copied, putErr := destination.Put(ctx, reader, metadata.MediaType)
		closeErr := reader.Close()
		if putErr != nil {
			return operation, manifest, artifactForkCompleteV1{}, "", "", putErr
		}
		if closeErr != nil {
			return operation, manifest, artifactForkCompleteV1{}, "", "", closeErr
		}
		if copied != metadata {
			return operation, manifest, artifactForkCompleteV1{}, "", "", fmt.Errorf("copied artifact metadata differs for %s", rawRef)
		}
		usage := inventory.Managed[rawRef]
		record := inventory.Records[rawRef]
		manifest.Objects = append(manifest.Objects, artifactForkManifestEntryV1{
			Ref: rawRef, Algorithm: metadata.Algorithm, Digest: metadata.Digest, MediaType: metadata.MediaType, Size: metadata.Size,
			Indexed: record.Ref != "", Accepted: usage.Ref != "", Backend: record.Backend,
			EventIDs: usage.EventIDs, Locations: usage.Locations,
		})
		manifest.CopiedByteCount += metadata.Size
		if err := invokeForkFault(options, "after-object-copy"); err != nil {
			return operation, manifest, artifactForkCompleteV1{}, "", "", err
		}
	}
	manifest.CopiedObjectCount = len(manifest.Objects)
	if source != nil {
		objects, err := source.Scan(ctx)
		if err != nil {
			return operation, manifest, artifactForkCompleteV1{}, "", "", err
		}
		for _, object := range objects {
			if object.Ref == "" || !object.Valid {
				return operation, manifest, artifactForkCompleteV1{}, "", "", fmt.Errorf("artifact-integrity-failure: source namespace contains an invalid CAS object: %v", object.Err)
			}
			if _, copied := required[object.Ref.String()]; !copied {
				manifest.ExcludedUnreferencedObjects = append(manifest.ExcludedUnreferencedObjects, object.Ref.String())
			}
		}
	}
	sort.Strings(manifest.ExcludedUnreferencedObjects)
	manifest.ExcludedUnreferencedObjCount = len(manifest.ExcludedUnreferencedObjects)
	if err := invokeForkFault(options, "after-artifact-copy"); err != nil {
		return operation, manifest, artifactForkCompleteV1{}, "", "", err
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return operation, manifest, artifactForkCompleteV1{}, "", "", err
	}
	manifestDigest := digestBytes(manifestBytes)
	if err := writeDurableFile(filepath.Join(stageRoot, artifactForkManifestFilename), manifestBytes); err != nil {
		return operation, manifest, artifactForkCompleteV1{}, "", "", err
	}
	if err := invokeForkFault(options, "after-manifest"); err != nil {
		return operation, manifest, artifactForkCompleteV1{}, "", "", err
	}
	complete := artifactForkCompleteV1{
		Version: "artifact-namespace-fork-complete-v1", FromStoreID: from.StoreID, ToStoreID: operation.ToStoreID,
		ManifestDigest: manifestDigest, OperationDigest: operationDigest, BackupDatabaseSHA256: backupDigest,
		CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	completeBytes, _ := json.Marshal(complete)
	completeDigest := digestBytes(completeBytes)
	if err := writeDurableFile(filepath.Join(stageRoot, artifactForkCompleteFilename), completeBytes); err != nil {
		return operation, manifest, complete, manifestDigest, "", err
	}
	if err := invokeForkFault(options, "after-completion-marker"); err != nil {
		return operation, manifest, complete, manifestDigest, completeDigest, err
	}
	if stageRoot != destinationRoot {
		if err := os.Rename(stageRoot, destinationRoot); err != nil {
			return operation, manifest, complete, manifestDigest, completeDigest, err
		}
		if err := fsutil.SyncDir(filepath.Dir(destinationRoot)); err != nil {
			return operation, manifest, complete, manifestDigest, completeDigest, err
		}
	}
	if err := invokeForkFault(options, "after-namespace-publish"); err != nil {
		return operation, manifest, complete, manifestDigest, completeDigest, err
	}
	return operation, manifest, complete, manifestDigest, completeDigest, nil
}

func validateArtifactForkOperation(operation artifactForkOperationV1, from IdentityInfo, head, integrityEpoch string, count int64, backupDigest string) error {
	if operation.Version != "artifact-namespace-fork-operation-v1" || operation.FromStoreID != from.StoreID ||
		operation.FromIdentityDocumentDigest != from.DocumentDigest || operation.FromHeadDigest != head || operation.FromHeadIntegrityEpoch != integrityEpoch || operation.FromEventCount != count ||
		operation.ArtifactForkProtocol != "artifact-namespace-fork-v1" || operation.ReceiptVersion != "store-identity-fork-v2" || operation.BackupDatabaseSHA256 != backupDigest {
		return errors.New("existing artifact fork staging belongs to a different source snapshot")
	}
	if operation.ToIdentityScheme != storeidentity.Scheme || storeidentity.DocumentDigest(operation.ToIdentityDocument) != operation.ToIdentityDocumentDigest {
		return errors.New("existing artifact fork staging contains an invalid child identity document")
	}
	return storeidentity.ValidateBinding(operation.ToStoreID, operation.ToIdentityDocument)
}

func invokeForkFault(options WritableForkOptions, stage string) error {
	if options.Fault == nil {
		return nil
	}
	if err := options.Fault(stage); err != nil {
		return fmt.Errorf("injected artifact fork fault at %s: %w", stage, err)
	}
	return nil
}

func writeDurableFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fork-metadata-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return fsutil.SyncDir(filepath.Dir(path))
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func declareWritableForkV2(ctx context.Context, db *sql.DB, plan WritableForkPlan, backupDigest string, operation artifactForkOperationV1, manifest artifactForkManifestV1, complete artifactForkCompleteV1, manifestDigest, completeDigest string) (string, string, string, error) {
	var from IdentityInfo
	var head, integrityEpoch string
	var count int64
	if err := db.QueryRowContext(ctx, `SELECT store_id,identity_scheme,document_bytes,document_digest,artifact_namespace FROM store_identity_v1 WHERE singleton=1`).Scan(
		&from.StoreID, &from.Scheme, &from.DocumentBytes, &from.DocumentDigest, &from.ArtifactNamespace,
	); err != nil {
		return "", "", "", err
	}
	if err := validateIdentityInfo(from); err != nil {
		return "", "", "", err
	}
	if err := db.QueryRowContext(ctx, `SELECT head_hash,integrity_epoch FROM store_meta WHERE singleton=1`).Scan(&head, &integrityEpoch); err != nil {
		return "", "", "", err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return "", "", "", err
	}
	if from.StoreID != plan.FromStoreID || head != operation.FromHeadDigest || count != operation.FromEventCount || complete.ManifestDigest != manifestDigest {
		return "", "", "", errors.New("source or published artifact manifest changed before database commit")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	base := writableForkReceiptV1{
		Version: "store-identity-fork-v2", FromStoreID: from.StoreID, FromIdentityScheme: from.Scheme,
		FromIdentityDocument: from.DocumentBytes, FromIdentityDocumentDigest: from.DocumentDigest,
		FromHeadDigest: head, FromHeadIntegrityEpoch: integrityEpoch, FromEventCount: count,
		FromFormatRevision: plan.FormatRevision, ToStoreID: operation.ToStoreID, ToIdentityScheme: storeidentity.Scheme,
		ToIdentityDocumentDigest: operation.ToIdentityDocumentDigest, ArtifactDisposition: "copied-independent-namespace-v1",
		ArtifactNamespace: operation.ToStoreID, BackupDatabaseSHA256: backupDigest, ForkedAt: now,
	}
	receipt := writableForkReceiptV2{
		writableForkReceiptV1: base, ArtifactManifestDigest: manifestDigest, CompletionMarkerDigest: completeDigest,
		CopiedObjectCount: manifest.CopiedObjectCount, CopiedByteCount: manifest.CopiedByteCount,
		UnmanagedReferenceCount: manifest.UnmanagedReferenceCount, ExcludedUnreferencedObjCount: manifest.ExcludedUnreferencedObjCount,
		UnmanagedDisposition: "provenance-only-unmanaged-v1", ExcludedObjectDisposition: "excluded-unreferenced-v1",
		FromArtifactNamespace: from.ArtifactNamespace, ArtifactForkProtocol: "artifact-namespace-fork-v1",
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return "", "", "", err
	}
	receiptDigest := digestBytes(receiptBytes)
	receiptID := "identity-fork:" + strings.TrimPrefix(receiptDigest, "sha256:")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO store_identity_migration_receipts(
		receipt_id,from_store_id,from_identity_scheme,to_store_id,to_identity_scheme,
		source_head_digest,source_head_integrity_epoch,source_event_count,source_format_revision,
		target_format_revision,artifact_namespace,backup_database_sha256,migrated_at,receipt_bytes,receipt_digest
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, receiptID, from.StoreID, from.Scheme, operation.ToStoreID, storeidentity.Scheme,
		head, integrityEpoch, count, plan.FormatRevision, plan.FormatRevision, operation.ToStoreID, backupDigest, now, receiptBytes, receiptDigest); err != nil {
		return "", "", "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO artifact_namespace_forks(
		receipt_id,from_store_id,to_store_id,manifest_digest,completion_marker_digest,copied_object_count,
		copied_byte_count,unmanaged_reference_count,excluded_object_count,completed_at
	) VALUES(?,?,?,?,?,?,?,?,?,?)`, receiptID, from.StoreID, operation.ToStoreID, manifestDigest, completeDigest,
		manifest.CopiedObjectCount, manifest.CopiedByteCount, manifest.UnmanagedReferenceCount, manifest.ExcludedUnreferencedObjCount, complete.CompletedAt); err != nil {
		return "", "", "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_identity_v1 SET store_id=?,identity_scheme=?,document_bytes=?,document_digest=?,artifact_namespace=?,created_at=?,creator_protocol=?,creator_contract_digest=NULL WHERE singleton=1`,
		operation.ToStoreID, storeidentity.Scheme, operation.ToIdentityDocument, operation.ToIdentityDocumentDigest, operation.ToStoreID, now, "eventstore-v3-alpha.4"); err != nil {
		return "", "", "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_meta SET store_id=?,updated_at=? WHERE singleton=1`, operation.ToStoreID, now); err != nil {
		return "", "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", "", err
	}
	return operation.ToStoreID, receiptID, receiptDigest, nil
}
