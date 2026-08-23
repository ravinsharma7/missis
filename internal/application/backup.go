package application

import (
	"context"
	"crypto/sha256"
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

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/fsutil"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// Manifest returns the current store fingerprint.
func (s *Service) Manifest(ctx context.Context) (missis.ManifestInfo, error) {
	return manifestInfoFromStore(ctx, s.store)
}

// BackupTo writes a snapshot and its logical artifact sidecars to dst.
func (s *Service) BackupTo(ctx context.Context, dst string) error {
	return s.Backup(ctx, dst)
}

// Restore restores either a logical backup bundle or a legacy database-only
// backup. The destination must not already exist.
func (s *Service) Restore(ctx context.Context, backupPath, dst string) error {
	return s.restoreWithOptions(ctx, backupPath, dst, missis.RestoreOptions{})
}

// RestoreWithOptions is Restore with an explicit artifact root override.
func (s *Service) RestoreWithOptions(ctx context.Context, backupPath, dst string, opts missis.RestoreOptions) error {
	return s.restoreWithOptions(ctx, backupPath, dst, opts)
}

// VerifyRestore validates a database-only backup or a complete logical bundle
// against the expected store fingerprint and artifact payload.
func (s *Service) VerifyRestore(ctx context.Context, backupPath string, expect missis.ManifestInfo) error {
	return s.verifyBackup(ctx, backupPath, expect)
}

// BackupState describes the publication state that can be determined without
// opening the database. Full integrity verification is still performed by
// VerifyRestore; a structurally complete bundle can therefore be classified
// as corrupt when its hashes or blobs fail verification.
type BackupState string

const (
	BackupStateComplete   BackupState = "complete"
	BackupStateLegacyV1   BackupState = "legacy-v1"
	BackupStateIncomplete BackupState = "incomplete"
	BackupStateCorrupt    BackupState = "corrupt"
)

func ClassifyBackup(path string) (BackupState, error) {
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return BackupStateCorrupt, fmt.Errorf("backup database is missing: %w", err)
		}
		return BackupStateCorrupt, err
	}
	manifest, err := readBackupManifest(path)
	if errors.Is(err, os.ErrNotExist) {
		if _, sidecarErr := os.Stat(backupArtifactsPath(path)); sidecarErr == nil {
			return BackupStateIncomplete, nil
		} else if !os.IsNotExist(sidecarErr) {
			return BackupStateCorrupt, sidecarErr
		}
		return BackupStateLegacyV1, nil
	}
	if err != nil {
		return BackupStateCorrupt, fmt.Errorf("read backup manifest: %w", err)
	}
	if manifest.Version == missis.BackupManifestVersionV1 {
		return BackupStateLegacyV1, nil
	}
	if manifest.Version != missis.BackupManifestVersion {
		return BackupStateCorrupt, fmt.Errorf("unsupported backup manifest version: %d", manifest.Version)
	}
	if _, err := os.Stat(backupCompletionPath(path)); errors.Is(err, os.ErrNotExist) {
		return BackupStateIncomplete, nil
	} else if err != nil {
		return BackupStateCorrupt, err
	}
	return BackupStateComplete, nil
}

func (s *Service) backupTo(ctx context.Context, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dst = filepath.Clean(strings.TrimSpace(dst))
	if dst == "." || dst == "" {
		return fmt.Errorf("backup destination is required")
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	manifestPath := backupManifestPath(dst)
	artifactsPath := backupArtifactsPath(dst)
	completionPath := backupCompletionPath(dst)
	for _, path := range []string{dst, manifestPath, artifactsPath, completionPath} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("backup destination already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	tmpDB, err := temporaryPath(dir, "."+filepath.Base(dst)+".db-")
	if err != nil {
		return err
	}
	defer os.Remove(tmpDB)
	if err := s.store.BackupContext(ctx, tmpDB); err != nil {
		return err
	}

	snapshot, err := store.OpenSnapshot(tmpDB)
	if err != nil {
		return fmt.Errorf("open backup snapshot: %w", err)
	}
	snapshotManifest, err := manifestInfoFromStore(ctx, snapshot)
	if err != nil {
		_ = snapshot.Close()
		return err
	}
	records, err := snapshot.ListArtifacts(ctx)
	closeErr := snapshot.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	stageArtifacts, err := os.MkdirTemp(dir, "."+filepath.Base(dst)+".artifacts-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageArtifacts)
	if err := os.Chmod(stageArtifacts, 0o700); err != nil {
		return err
	}
	artifactSnapshot, err := artifact.NewLocalStore(stageArtifacts)
	if err != nil {
		return err
	}
	entries := make([]missis.BackupArtifactEntry, 0, len(records))
	for _, record := range records {
		entry, err := s.copyArtifactToSnapshot(ctx, artifactSnapshot, record)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref < entries[j].Ref })

	databaseInfo, err := fileInfo(tmpDB)
	if err != nil {
		return err
	}
	manifest := missis.BackupManifest{
		Version:      missis.BackupManifestVersion,
		ArtifactMode: missis.BackupArtifactEmbedded,
		Database:     databaseInfo,
		Store:        snapshotManifest,
		Artifacts:    entries,
	}
	tmpManifest, err := temporaryPath(dir, "."+filepath.Base(manifestPath)+"-")
	if err != nil {
		return err
	}
	defer os.Remove(tmpManifest)
	if err := writeJSONFile(tmpManifest, manifest); err != nil {
		return err
	}

	publishedArtifacts := false
	publishedManifest := false
	defer func() {
		if !publishedArtifacts {
			_ = os.RemoveAll(artifactsPath)
		}
		if !publishedManifest {
			_ = os.Remove(manifestPath)
		}
	}()
	if err := os.Rename(stageArtifacts, artifactsPath); err != nil {
		return err
	}
	publishedArtifacts = true
	if err := os.Rename(tmpManifest, manifestPath); err != nil {
		return err
	}
	publishedManifest = true
	if err := os.Rename(tmpDB, dst); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return err
	}
	manifestSHA, _, err := hashFile(manifestPath)
	if err != nil {
		return err
	}
	completion := missis.BackupCompletion{
		DatabaseSHA256: databaseInfo.SHA256,
		ManifestSHA256: manifestSHA,
		BundleVersion:  manifest.Version,
		CompletedAt:    time.Now().UTC(),
	}
	tmpCompletion, err := temporaryPath(dir, "."+filepath.Base(completionPath)+"-")
	if err != nil {
		return err
	}
	defer os.Remove(tmpCompletion)
	if err := writeJSONFile(tmpCompletion, completion); err != nil {
		return err
	}
	if err := os.Rename(tmpCompletion, completionPath); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func (s *Service) copyArtifactToSnapshot(ctx context.Context, target *artifact.LocalStore, record store.ArtifactRecord) (missis.BackupArtifactEntry, error) {
	ref, err := artifact.ParseRef(record.Ref)
	if err != nil {
		return missis.BackupArtifactEntry{}, err
	}
	reader, err := s.artifacts.Open(ctx, ref)
	if err != nil {
		return missis.BackupArtifactEntry{}, fmt.Errorf("open artifact %s for backup: %w", record.Ref, err)
	}
	metadata, putErr := target.Put(ctx, reader, record.MediaType)
	closeErr := reader.Close()
	if putErr != nil {
		return missis.BackupArtifactEntry{}, fmt.Errorf("copy artifact %s: %w", record.Ref, putErr)
	}
	if closeErr != nil {
		return missis.BackupArtifactEntry{}, closeErr
	}
	if metadata.Ref != ref || metadata.Algorithm != record.Algorithm || metadata.Digest != record.Digest || metadata.MediaType != record.MediaType || metadata.Size != record.Size {
		return missis.BackupArtifactEntry{}, fmt.Errorf("artifact %s changed while backing up", record.Ref)
	}
	return missis.BackupArtifactEntry{
		Ref:       record.Ref,
		Algorithm: record.Algorithm,
		Digest:    record.Digest,
		MediaType: record.MediaType,
		Size:      record.Size,
		Backend:   record.Backend,
	}, nil
}

func (s *Service) restoreWithOptions(ctx context.Context, backupPath, dst string, opts missis.RestoreOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	backupPath = filepath.Clean(backupPath)
	dst = filepath.Clean(dst)
	backupLease, err := acquireBackupReadLease(backupPath)
	if err != nil {
		return err
	}
	if backupLease != nil {
		defer backupLease.Close()
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("restore destination already exists; restore to a new path: %s", dst)
	} else if !os.IsNotExist(err) {
		return err
	}
	destinationLease, err := store.AcquireExclusiveLease(dst)
	if err != nil {
		return fmt.Errorf("acquire exclusive restore destination lease: %w", err)
	}
	defer destinationLease.Close()
	manifest, err := readBackupManifest(backupPath)
	if errors.Is(err, os.ErrNotExist) {
		if _, sidecarErr := os.Stat(backupArtifactsPath(backupPath)); sidecarErr == nil {
			return fmt.Errorf("incomplete backup bundle: artifact sidecar exists without a manifest")
		} else if !os.IsNotExist(sidecarErr) {
			return sidecarErr
		}
		return restoreLegacyDatabase(ctx, backupPath, dst)
	}
	if err != nil {
		return err
	}
	if err := validateBackupManifest(manifest); err != nil {
		return err
	}
	if err := verifyBackupCompletion(backupPath, manifest); err != nil {
		return err
	}
	if manifest.ArtifactMode != missis.BackupArtifactEmbedded {
		return fmt.Errorf("backup artifact mode %q is not supported by the local restore backend", manifest.ArtifactMode)
	}
	if err := verifyFileInfo(backupPath, manifest.Database); err != nil {
		return fmt.Errorf("backup database verification failed: %w", err)
	}

	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmpDB, err := temporaryPath(dir, "."+filepath.Base(dst)+".restore-")
	if err != nil {
		return err
	}
	defer os.Remove(tmpDB)
	if err := copyFileContext(ctx, backupPath, tmpDB); err != nil {
		return err
	}
	if err := verifyRestoredDatabase(ctx, tmpDB, manifest.Store, manifest.Artifacts); err != nil {
		return err
	}

	backupArtifacts, err := openBackupArtifactStore(backupArtifactsPath(backupPath))
	if err != nil {
		return fmt.Errorf("open backup artifacts: %w", err)
	}
	artifactRoot, err := restoreArtifactRoot(manifest.Store.StoreID, opts.ArtifactRoot)
	if err != nil {
		return err
	}
	artifactLease, err := store.AcquireExclusiveLease(artifactRoot)
	if err != nil {
		return fmt.Errorf("acquire exclusive restore artifact lease: %w", err)
	}
	defer artifactLease.Close()
	if err := os.MkdirAll(filepath.Dir(artifactRoot), 0o700); err != nil {
		return err
	}
	stageArtifacts, err := os.MkdirTemp(filepath.Dir(artifactRoot), ".restore-artifacts-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageArtifacts)
	stageStore, err := artifact.NewLocalStore(stageArtifacts)
	if err != nil {
		return err
	}
	for _, entry := range manifest.Artifacts {
		if err := copyArtifactFromSnapshot(ctx, backupArtifacts, stageStore, entry); err != nil {
			return err
		}
	}
	if err := publishArtifactRoot(ctx, stageStore, stageArtifacts, artifactRoot, manifest.Artifacts); err != nil {
		return err
	}
	if err := os.Rename(tmpDB, dst); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func copyArtifactFromSnapshot(ctx context.Context, source, target *artifact.LocalStore, entry missis.BackupArtifactEntry) error {
	ref, err := artifact.ParseRef(entry.Ref)
	if err != nil {
		return err
	}
	reader, err := source.Open(ctx, ref)
	if err != nil {
		return fmt.Errorf("open backup artifact %s: %w", entry.Ref, err)
	}
	metadata, putErr := target.Put(ctx, reader, entry.MediaType)
	closeErr := reader.Close()
	if putErr != nil {
		return putErr
	}
	if closeErr != nil {
		return closeErr
	}
	if metadata.Ref != ref || metadata.Algorithm != entry.Algorithm || metadata.Digest != entry.Digest || metadata.MediaType != entry.MediaType || metadata.Size != entry.Size {
		return fmt.Errorf("backup artifact %s failed digest or size verification", entry.Ref)
	}
	return nil
}

func publishArtifactRoot(ctx context.Context, staged *artifact.LocalStore, stageRoot, root string, entries []missis.BackupArtifactEntry) error {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		if err := os.Rename(stageRoot, root); err != nil {
			return err
		}
		return nil
	} else if err != nil {
		return err
	}
	target, err := artifact.NewLocalStore(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		ref, err := artifact.ParseRef(entry.Ref)
		if err != nil {
			return err
		}
		existing, statErr := target.Stat(ctx, ref)
		if statErr == nil {
			if existing.Digest != entry.Digest || existing.Size != entry.Size {
				return fmt.Errorf("existing artifact %s does not match backup", entry.Ref)
			}
			continue
		}
		reader, err := staged.Open(ctx, ref)
		if err != nil {
			return err
		}
		_, putErr := target.Put(ctx, reader, entry.MediaType)
		closeErr := reader.Close()
		if putErr != nil {
			return putErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func restoreLegacyDatabase(ctx context.Context, backupPath, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := temporaryPath(filepath.Dir(dst), "."+filepath.Base(dst)+".restore-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := copyFileContext(ctx, backupPath, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(dst))
}

func (s *Service) verifyBackup(ctx context.Context, backupPath string, expect missis.ManifestInfo) error {
	backupLease, err := acquireBackupReadLease(backupPath)
	if err != nil {
		return err
	}
	if backupLease != nil {
		defer backupLease.Close()
	}
	manifest, err := readBackupManifest(backupPath)
	if errors.Is(err, os.ErrNotExist) {
		if _, sidecarErr := os.Stat(backupArtifactsPath(backupPath)); sidecarErr == nil {
			return fmt.Errorf("incomplete backup bundle: artifact sidecar exists without a manifest")
		} else if !os.IsNotExist(sidecarErr) {
			return sidecarErr
		}
		got, verifyErr := manifestFromDatabase(ctx, backupPath)
		if verifyErr != nil {
			return verifyErr
		}
		return compareManifest(got, expect)
	}
	if err != nil {
		return err
	}
	if err := validateBackupManifest(manifest); err != nil {
		return err
	}
	if err := verifyBackupCompletion(backupPath, manifest); err != nil {
		return err
	}
	if err := verifyFileInfo(backupPath, manifest.Database); err != nil {
		return err
	}
	got, err := manifestFromDatabase(ctx, backupPath)
	if err != nil {
		return err
	}
	if err := compareManifest(got, expect); err != nil {
		return err
	}
	if err := compareManifest(got, manifest.Store); err != nil {
		return err
	}
	if manifest.ArtifactMode != missis.BackupArtifactEmbedded {
		return fmt.Errorf("unsupported backup artifact mode: %s", manifest.ArtifactMode)
	}
	if manifest.ArtifactMode == missis.BackupArtifactEmbedded {
		if _, err := os.Stat(backupArtifactsPath(backupPath)); err != nil {
			return fmt.Errorf("incomplete backup bundle: artifact sidecar is unavailable: %w", err)
		}
	}
	snapshot, err := openBackupArtifactStore(backupArtifactsPath(backupPath))
	if err != nil {
		return err
	}
	for _, entry := range manifest.Artifacts {
		ref, err := artifact.ParseRef(entry.Ref)
		if err != nil {
			return err
		}
		reader, err := snapshot.Open(ctx, ref)
		if err != nil {
			return fmt.Errorf("backup artifact %s is missing: %w", entry.Ref, err)
		}
		hashed, hashErr := hashReader(ctx, reader)
		closeErr := reader.Close()
		if hashErr != nil {
			return hashErr
		}
		if closeErr != nil {
			return closeErr
		}
		if hashed.Digest != entry.Digest || hashed.Size != entry.Size {
			return fmt.Errorf("backup artifact %s failed verification", entry.Ref)
		}
	}
	return verifyRestoredDatabase(ctx, backupPath, manifest.Store, manifest.Artifacts)
}

func acquireBackupReadLease(path string) (*store.Lease, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lease, err := store.AcquireSharedLease(path)
	if err != nil {
		return nil, fmt.Errorf("acquire shared backup lease path=%q: %w", path, err)
	}
	return lease, nil
}

func verifyRestoredDatabase(ctx context.Context, path string, expected missis.ManifestInfo, entries []missis.BackupArtifactEntry) error {
	actual, err := manifestFromDatabase(ctx, path)
	if err != nil {
		return err
	}
	if err := compareManifest(actual, expected); err != nil {
		return err
	}
	snapshot, err := store.OpenSnapshot(path)
	if err != nil {
		return err
	}
	records, err := snapshot.ListArtifacts(ctx)
	closeErr := snapshot.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if len(records) != len(entries) {
		return fmt.Errorf("backup artifact index count %d does not match manifest count %d", len(records), len(entries))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Ref < records[j].Ref })
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref < entries[j].Ref })
	for i, record := range records {
		if record.Ref != entries[i].Ref || record.Algorithm != entries[i].Algorithm || record.Digest != entries[i].Digest || record.MediaType != entries[i].MediaType || record.Size != entries[i].Size || record.Backend != entries[i].Backend {
			return fmt.Errorf("backup artifact index entry %s does not match manifest", record.Ref)
		}
	}
	return nil
}

func manifestFromDatabase(ctx context.Context, path string) (missis.ManifestInfo, error) {
	db, err := store.OpenSnapshot(path)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	manifest, err := manifestInfoFromStore(ctx, db)
	closeErr := db.Close()
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	if closeErr != nil {
		return missis.ManifestInfo{}, closeErr
	}
	return manifest, nil
}

func manifestInfoFromStore(ctx context.Context, db *store.Store) (missis.ManifestInfo, error) {
	storeID, err := db.StoreIDContext(ctx)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	head, err := db.HeadHashContext(ctx)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	count, err := db.EventCountContext(ctx)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	schemaVersion, err := db.SchemaVersionContext(ctx)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	return missis.ManifestInfo{StoreID: storeID, HeadHash: head, EventCount: count, SchemaVersion: schemaVersion}, nil
}

func compareManifest(actual, expected missis.ManifestInfo) error {
	if actual != expected {
		return fmt.Errorf("restore verification failed: store_id %q/%q head %q/%q schema %q/%q events %d/%d", actual.StoreID, expected.StoreID, actual.HeadHash, expected.HeadHash, actual.SchemaVersion, expected.SchemaVersion, actual.EventCount, expected.EventCount)
	}
	return nil
}

func validateBackupManifest(manifest missis.BackupManifest) error {
	if manifest.Version != missis.BackupManifestVersion && manifest.Version != missis.BackupManifestVersionV1 {
		return fmt.Errorf("unsupported backup manifest version: %d", manifest.Version)
	}
	if manifest.ArtifactMode != missis.BackupArtifactEmbedded && manifest.ArtifactMode != missis.BackupArtifactExternal {
		return fmt.Errorf("unsupported backup artifact mode: %q", manifest.ArtifactMode)
	}
	if !validSHA256(manifest.Database.SHA256) || manifest.Database.Size < 0 {
		return fmt.Errorf("backup database metadata is incomplete")
	}
	if strings.TrimSpace(manifest.Store.StoreID) == "" || strings.TrimSpace(manifest.Store.SchemaVersion) == "" || manifest.Store.EventCount < 0 {
		return fmt.Errorf("backup store metadata is incomplete")
	}
	if manifest.Store.EventCount > 0 && strings.TrimSpace(manifest.Store.HeadHash) == "" {
		return fmt.Errorf("backup store metadata is incomplete: event head is missing")
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	for _, entry := range manifest.Artifacts {
		ref, err := artifact.ParseRef(entry.Ref)
		if err != nil {
			return fmt.Errorf("invalid backup artifact reference %q: %w", entry.Ref, err)
		}
		if _, exists := seen[ref.String()]; exists {
			return fmt.Errorf("duplicate backup artifact reference: %s", ref)
		}
		seen[ref.String()] = struct{}{}
		wantDigest := strings.TrimPrefix(ref.String(), "artifact:sha256:")
		if entry.Algorithm != "sha256" || !validSHA256(entry.Digest) || entry.Digest != wantDigest || entry.Size < 0 || strings.TrimSpace(entry.Backend) == "" {
			return fmt.Errorf("invalid metadata for backup artifact %s", ref)
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func openBackupArtifactStore(path string) (*artifact.LocalStore, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("incomplete backup bundle: artifact sidecar is missing: %w", err)
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("incomplete backup bundle: artifact sidecar is not a directory: %s", path)
	}
	return artifact.NewLocalStore(path)
}

func readBackupManifest(backupPath string) (missis.BackupManifest, error) {
	file, err := os.Open(backupManifestPath(backupPath))
	if err != nil {
		return missis.BackupManifest{}, err
	}
	defer file.Close()
	var manifest missis.BackupManifest
	if err := json.NewDecoder(file).Decode(&manifest); err != nil {
		return missis.BackupManifest{}, err
	}
	return manifest, nil
}

func backupManifestPath(dst string) string { return dst + ".manifest.json" }

func backupArtifactsPath(dst string) string { return dst + ".artifacts" }

func backupCompletionPath(dst string) string { return dst + ".complete.json" }

func verifyBackupCompletion(backupPath string, manifest missis.BackupManifest) error {
	if manifest.Version < missis.BackupManifestVersion {
		return nil
	}
	file, err := os.Open(backupCompletionPath(backupPath))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("incomplete backup bundle: completion marker is missing")
		}
		return err
	}
	var completion missis.BackupCompletion
	decodeErr := json.NewDecoder(file).Decode(&completion)
	closeErr := file.Close()
	if decodeErr != nil {
		return fmt.Errorf("decode backup completion marker: %w", decodeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	manifestInfo, err := fileInfo(backupManifestPath(backupPath))
	if err != nil {
		return err
	}
	if completion.DatabaseSHA256 != manifest.Database.SHA256 || completion.ManifestSHA256 != manifestInfo.SHA256 || completion.BundleVersion != manifest.Version || completion.CompletedAt.IsZero() {
		return fmt.Errorf("backup completion marker does not match the published bundle")
	}
	return nil
}

func restoreArtifactRoot(storeID, override string) (string, error) {
	if strings.TrimSpace(override) == "" {
		override = os.Getenv("MISSIS_ARTIFACT_STORE")
	}
	if strings.TrimSpace(override) != "" {
		return filepath.Clean(override), nil
	}
	return namespacedArtifactRoot(storeID)
}

func namespacedArtifactRoot(storeID string) (string, error) {
	dataDir, err := platformUserDataDir()
	if err != nil || strings.TrimSpace(dataDir) == "" {
		if err == nil {
			err = fmt.Errorf("user data directory is empty")
		}
		return "", fmt.Errorf("cannot resolve artifact root: %w; set MISSIS_ARTIFACT_STORE", err)
	}
	digest := sha256.Sum256([]byte(storeID))
	return filepath.Join(dataDir, "missis", "artifacts", hex.EncodeToString(digest[:16])), nil
}

func temporaryPath(dir, pattern string) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func writeJSONFile(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encodeErr := json.NewEncoder(file).Encode(value)
	if encodeErr == nil {
		encodeErr = file.Sync()
	}
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func fileInfo(path string) (missis.BackupFileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return missis.BackupFileInfo{}, err
	}
	digest, size, err := hashFile(path)
	if err != nil {
		return missis.BackupFileInfo{}, err
	}
	if size != info.Size() {
		return missis.BackupFileInfo{}, fmt.Errorf("file changed while hashing: %s", path)
	}
	return missis.BackupFileInfo{SHA256: digest, Size: size}, nil
}

func verifyFileInfo(path string, expected missis.BackupFileInfo) error {
	actual, err := fileInfo(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("file %s does not match backup manifest", path)
	}
	return nil
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	hashed, hashErr := hashReader(context.Background(), file)
	closeErr := file.Close()
	if hashErr != nil {
		return "", 0, hashErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	return hashed.Digest, hashed.Size, nil
}

type hashedContent struct {
	Digest string
	Size   int64
}

func hashReader(ctx context.Context, reader io.Reader) (hashedContent, error) {
	hasher := sha256.New()
	size, err := copyContext(ctx, hasher, reader)
	if err != nil {
		return hashedContent{}, err
	}
	return hashedContent{Digest: hex.EncodeToString(hasher.Sum(nil)), Size: size}, nil
}

func copyFileContext(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := copyContext(ctx, out, in)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	var total int64
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			written, writeErr := dst.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func syncDirectory(path string) error {
	return fsutil.SyncDir(path)
}
