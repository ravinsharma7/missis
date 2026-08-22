package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/store"
)

func TestDefaultArtifactRootIsNamespacedOutsideProject(t *testing.T) {
	project := filepath.Join(t.TempDir(), strings.Repeat("long-project-segment-", 10), "project")
	path := filepath.Join(project, "missis.db")
	svc, err := OpenPathWithClock(path, fixedClock{fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	root := svc.ArtifactRoot()
	if root == filepath.Join(project, "artifacts") || strings.Contains(root, project) {
		t.Fatalf("artifact root %q is project-local or path-derived", root)
	}
	if !strings.Contains(root, filepath.Join("missis", "artifacts")) {
		t.Fatalf("artifact root %q is missing the missis artifact namespace", root)
	}
	if _, err := svc.ArtifactStore().Put(context.Background(), strings.NewReader("data"), "text/plain"); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyArtifactRootIsUsedWithWarning(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(filepath.Join(legacy, "sha256"), 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := OpenPathWithClock(filepath.Join(dir, "missis.db"), fixedClock{fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if svc.ArtifactRoot() != legacy {
		t.Fatalf("artifact root = %q, want legacy %q", svc.ArtifactRoot(), legacy)
	}
	if svc.ArtifactRootWarning() == "" {
		t.Fatal("expected legacy artifact migration warning")
	}
}

func TestLegacyAndNamespacedArtifactRootsRequireExplicitMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missis.db")
	svc, err := OpenPathWithClock(path, fixedClock{fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	namespaced := svc.ArtifactRoot()
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "artifacts", "sha256"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = OpenPathWithClock(path, fixedClock{fixedNow()})
	if err == nil {
		t.Fatal("expected ambiguous legacy and namespaced artifact roots to fail")
	}
	if !strings.Contains(err.Error(), namespaced) || !strings.Contains(err.Error(), "MISSIS_ARTIFACT_STORE") {
		t.Fatalf("migration error = %v", err)
	}
}

func TestExplicitOverlongArtifactRootIsActionable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missis.db")
	root := filepath.Join(t.TempDir(), strings.Repeat("r", 300))
	_, err := OpenPathWithClockAndArtifactRoot(path, fixedClock{fixedNow()}, root)
	if !errors.Is(err, artifact.ErrPathTooLong) {
		t.Fatalf("error = %v, want ErrPathTooLong", err)
	}
	if !strings.Contains(err.Error(), "MISSIS_ARTIFACT_STORE") {
		t.Fatalf("error = %v, want override guidance", err)
	}
}

func TestApplicationServiceHoldsSharedMaintenanceLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missis.db")
	svc, err := OpenPathWithClock(path, fixedClock{fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcquireExclusiveLease(path); !errors.Is(err, store.ErrMaintenanceBusy) {
		t.Fatalf("exclusive lease error = %v, want ErrMaintenanceBusy", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireExclusiveLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}
