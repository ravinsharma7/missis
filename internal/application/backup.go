package application

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ravinsharma7/missis/pkg/missis"
)

// Manifest returns the current store fingerprint.
func (s *Service) Manifest(ctx context.Context) (missis.ManifestInfo, error) {
	storeID, err := s.StoreIDContext(ctx)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	headHash, err := s.HeadHashContext(ctx)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	eventCount, err := s.EventCountContext(ctx)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	schemaVersion, err := s.SchemaVersionContext(ctx)
	if err != nil {
		return missis.ManifestInfo{}, err
	}
	return missis.ManifestInfo{
		SchemaVersion: schemaVersion,
		StoreID:       storeID,
		HeadHash:      headHash,
		EventCount:    eventCount,
	}, nil
}

// BackupTo writes a consistent copy of the store to dst.
func (s *Service) BackupTo(ctx context.Context, dst string) error {
	return s.Backup(ctx, dst)
}

// Restore copies a backup file into dst.
func (s *Service) Restore(ctx context.Context, backupPath, dst string) error {
	src, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return err
	}
	return out.Sync()
}

// VerifyRestore opens backupPath as a store and checks its manifest against
// the expected fingerprint.
func (s *Service) VerifyRestore(ctx context.Context, backupPath string, expect missis.ManifestInfo) error {
	restored, err := OpenPath(backupPath)
	if err != nil {
		return err
	}
	defer restored.Close()
	got, err := restored.Manifest(ctx)
	if err != nil {
		return err
	}
	if got.StoreID != expect.StoreID || got.HeadHash != expect.HeadHash {
		return fmt.Errorf("restore verification failed: store_id %q/%q head %q/%q", got.StoreID, expect.StoreID, got.HeadHash, expect.HeadHash)
	}
	if got.SchemaVersion != expect.SchemaVersion || got.EventCount != expect.EventCount {
		return fmt.Errorf("restore verification failed: schema %q/%q events %d/%d", got.SchemaVersion, expect.SchemaVersion, got.EventCount, expect.EventCount)
	}
	return nil
}
