package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func newTestStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store.db")
	svc, err := application.OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVersionJSONIncludesSharedStoreFormat(t *testing.T) {
	// covers PH1-REL-001 PH1-FMT-001
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("version code=%d stderr=%s", code, stderr.String())
	}
	var info struct {
		Version             string `json:"version"`
		StoreFormatRevision int    `json:"store_format_revision"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Version == "" || info.StoreFormatRevision != store.CurrentStoreFormatRevision {
		t.Fatalf("version info = %#v", info)
	}
}

func TestArtifactMigrationIsIdempotentAndQuarantinesLegacyRoot(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "user-data"))
	storePath := newTestStore(t)
	legacyRoot := filepath.Join(filepath.Dir(storePath), "artifacts")
	legacy, err := artifact.NewLocalStore(legacyRoot)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := legacy.Put(context.Background(), bytes.NewBufferString("legacy bytes"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecordArtifact(context.Background(), store.ArtifactRecord{Ref: metadata.Ref.String(), Algorithm: metadata.Algorithm, Digest: metadata.Digest, MediaType: metadata.MediaType, Size: metadata.Size, Backend: "local"}); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCommand(t, "artifacts", "migrate", "--store", storePath, "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("migration: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var report struct {
		Status      string `json:"status"`
		Quarantine  string `json:"quarantine"`
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "migrated" || report.Quarantine == "" || report.Destination == "" {
		t.Fatalf("migration report = %+v", report)
	}
	if _, err := os.Stat(legacyRoot); !os.IsNotExist(err) {
		t.Fatalf("legacy root still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(report.Quarantine, "sha256")); err != nil {
		t.Fatalf("quarantined legacy root missing: %v", err)
	}
	target, err := artifact.NewLocalStore(report.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Verify(context.Background(), metadata.Ref); err != nil {
		t.Fatalf("migrated object: %v", err)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "migrate", "--store", storePath, "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "not-needed") {
		t.Fatalf("repeat migration: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestArtifactGCDefaultsToDryRunAndPreservesIndexedObjects(t *testing.T) {
	storePath := newTestStore(t)
	root := filepath.Join(t.TempDir(), "artifact-root")
	local, err := artifact.NewLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	indexed, err := local.Put(context.Background(), bytes.NewBufferString("indexed"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := local.Put(context.Background(), bytes.NewBufferString("orphan"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecordArtifact(context.Background(), store.ArtifactRecord{Ref: indexed.Ref.String(), Algorithm: indexed.Algorithm, Digest: indexed.Digest, MediaType: indexed.MediaType, Size: indexed.Size, Backend: "local"}); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCommand(t, "artifacts", "gc", "--store", storePath, "--artifact-root", root, "--grace", "0s", "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "dry-run") {
		t.Fatalf("gc dry-run: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if exists, err := local.Exists(context.Background(), orphan.Ref); err != nil || !exists {
		t.Fatalf("dry-run removed orphan: exists=%v err=%v", exists, err)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "gc", "--store", storePath, "--artifact-root", root, "--grace", "0s", "--confirm", "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "deleted") {
		t.Fatalf("gc confirm: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if exists, err := local.Exists(context.Background(), orphan.Ref); err != nil || exists {
		t.Fatalf("orphan remains after gc: exists=%v err=%v", exists, err)
	}
	if exists, err := local.Exists(context.Background(), indexed.Ref); err != nil || !exists {
		t.Fatalf("indexed object removed: exists=%v err=%v", exists, err)
	}
}

func TestArtifactMaintenanceRejectsActiveStoreClients(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.db")
	svc, err := application.OpenPath(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	code, stdout, stderr := runCommand(t, "artifacts", "migrate", "--store", storePath, "--json")
	if code == 0 || stdout != "" || stderr == "" || !strings.Contains(stderr, "maintenance_busy") {
		t.Fatalf("active migration: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "gc", "--store", storePath, "--artifact-root", filepath.Join(t.TempDir(), "artifacts"), "--grace", "0s", "--json")
	if code == 0 || stdout != "" || stderr == "" || !strings.Contains(stderr, "maintenance_busy") {
		t.Fatalf("active gc: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestArtifactVerifyReplaysExactEventsAndDetectsSameSizeTampering(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.db")
	root := filepath.Join(dir, "artifacts")
	svc, err := application.OpenPathWithClockAndArtifactRoot(storePath, toolingTestClock{}, root)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	created, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "verify artifact"})
	if err != nil {
		t.Fatal(err)
	}
	ingested, err := client.Ingest(context.Background(), missis.RequestContext{Actor: "test"}, missis.IngestOptions{
		Target: created.ID, MediaType: "application/octet-stream", SourceName: "evidence.bin", Content: strings.NewReader("original-bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCommand(t, "artifacts", "verify", "--store", storePath, "--artifact-root", root, "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"status":"verified"`) || !strings.Contains(stdout, ingested.Artifact) || !strings.Contains(stdout, `"event_ids"`) {
		t.Fatalf("healthy verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	digest := strings.TrimPrefix(ingested.Artifact, "artifact:sha256:")
	dataPath := filepath.Join(root, "sha256", digest[:2], digest[2:4], digest)
	if err := os.WriteFile(dataPath, []byte("modified-bytes"), 0o600); err != nil { // same length as original
		t.Fatal(err)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "verify", "--store", storePath, "--artifact-root", root, "--json")
	if code == 0 || stderr != "" || !strings.Contains(stdout, `"status":"inconsistent"`) || !strings.Contains(stdout, "corrupt-object") || !strings.Contains(stdout, "computed sha256=") {
		t.Fatalf("tampered verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if err := os.WriteFile(dataPath, []byte("original-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "verify", "--store", storePath, "--artifact-root", root, "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"status":"verified"`) {
		t.Fatalf("restored verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if err := os.Remove(dataPath); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "verify", "--store", storePath, "--artifact-root", root, "--json")
	if code == 0 || stderr != "" || !strings.Contains(stdout, "missing-object") || !strings.Contains(stdout, ingested.Artifact) || !strings.Contains(stdout, `"event_ids"`) {
		t.Fatalf("missing object verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if err := os.WriteFile(dataPath, []byte("original-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "verify", "--store", storePath, "--artifact-root", root, "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"status":"verified"`) {
		t.Fatalf("restored missing object verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

type toolingTestClock struct{}

func (toolingTestClock) Now() time.Time {
	return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
}

func TestArtifactVerifyClassifiesUnreferencedObjectWithoutGuessing(t *testing.T) {
	dir := t.TempDir()
	storePath := newTestStore(t)
	root := filepath.Join(dir, "artifacts")
	local, err := artifact.NewLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := local.Put(context.Background(), strings.NewReader("unreferenced"), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCommand(t, "artifacts", "verify", "--store", storePath, "--artifact-root", root, "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, metadata.Ref.String()) || !strings.Contains(stdout, `"status":"unreferenced-object"`) {
		t.Fatalf("unreferenced verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestArtifactVerifyFindsMissingIndexAndGCStillPreservesAcceptedObject(t *testing.T) {
	// covers PH1-ART-001 PH1-ART-002
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.db")
	root := filepath.Join(dir, "artifacts")
	svc, err := application.OpenPathWithClockAndArtifactRoot(storePath, toolingTestClock{}, root)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	created, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "missing artifact index"})
	if err != nil {
		t.Fatal(err)
	}
	ingested, err := client.Ingest(context.Background(), missis.RequestContext{Actor: "test"}, missis.IngestOptions{
		Target: created.ID, MediaType: "application/octet-stream", SourceName: "evidence.bin", Content: strings.NewReader("accepted-object"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM artifacts WHERE ref=?`, ingested.Artifact); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCommand(t, "artifacts", "verify", "--store", storePath, "--artifact-root", root, "--json")
	if code == 0 || stderr != "" || !strings.Contains(stdout, "missing-index") || !strings.Contains(stdout, `"event_ids"`) {
		t.Fatalf("missing index verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "gc", "--store", storePath, "--artifact-root", root, "--grace", "0s", "--confirm", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("gc after missing index: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	local, err := artifact.OpenLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := artifact.ParseRef(ingested.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Verify(context.Background(), ref); err != nil {
		t.Fatalf("GC deleted accepted object with missing index: %v", err)
	}
	rebuiltPath := filepath.Join(dir, "rebuilt.db")
	code, stdout, stderr = runCommand(t, "artifacts", "rebuild-index-copy", "--store", storePath, "--artifact-root", root, "--destination", rebuiltPath, "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"status":"rebuilt-copy"`) || !strings.Contains(stdout, `"rebuilt_index_rows":1`) {
		t.Fatalf("rebuild copy: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "verify", "--store", rebuiltPath, "--artifact-root", root, "--json")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"status":"verified"`) || strings.Contains(stdout, "missing-index") {
		t.Fatalf("rebuilt copy verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	sourceDB, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatal(err)
	}
	var sourceRows int
	if err := sourceDB.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&sourceRows); err != nil {
		_ = sourceDB.Close()
		t.Fatal(err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatal(err)
	}
	if sourceRows != 0 {
		t.Fatalf("source index was mutated: rows=%d", sourceRows)
	}
}

func TestArtifactCrashStagingIsReportedThenExplicitlyCollected(t *testing.T) {
	storePath := newTestStore(t)
	root := filepath.Join(t.TempDir(), "artifacts")
	if _, err := artifact.NewLocalStore(root); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(root, ".artifact-injectedtmp")
	if err := os.WriteFile(staging, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCommand(t, "artifacts", "verify", "--store", storePath, "--artifact-root", root, "--json")
	var verification struct {
		Status       string   `json:"status"`
		StagingPaths []string `json:"staging_paths"`
	}
	decodeErr := json.Unmarshal([]byte(stdout), &verification)
	if code != 0 || stderr != "" || decodeErr != nil || verification.Status != "verified-with-recoverable-staging" || len(verification.StagingPaths) != 1 || filepath.Clean(verification.StagingPaths[0]) != filepath.Clean(staging) {
		t.Fatalf("staging verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCommand(t, "artifacts", "gc", "--store", storePath, "--artifact-root", root, "--grace", "0s", "--confirm", "--json")
	var gc struct {
		Status    string   `json:"status"`
		StaleTemp []string `json:"stale_temp"`
	}
	decodeErr = json.Unmarshal([]byte(stdout), &gc)
	if code != 0 || stderr != "" || decodeErr != nil || gc.Status != "deleted" || len(gc.StaleTemp) != 1 || filepath.Clean(gc.StaleTemp[0]) != filepath.Clean(staging) {
		t.Fatalf("staging cleanup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging file remains: %v", err)
	}
}

func TestBackupVerifyAndCleanupRecognizeCompletionMarker(t *testing.T) {
	storePath := newTestStore(t)
	t.Setenv("MISSIS_STORE", storePath)
	directory := t.TempDir()
	backup := filepath.Join(directory, "backup.db")
	if code, _, stderr := runCommand(t, "backup", backup); code != 0 || stderr != "" {
		t.Fatalf("backup: code=%d stderr=%q", code, stderr)
	}
	if code, stdout, stderr := runCommand(t, "backup", "verify", backup); code != 0 || stdout != "state=complete: backup verified\n" || stderr != "" {
		t.Fatalf("backup verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if err := os.Remove(backup + ".complete.json"); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runCommand(t, "backup", "verify", backup); code == 0 || !strings.Contains(stderr, "completion marker") {
		t.Fatalf("incomplete verify: code=%d stderr=%q", code, stderr)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, path := range []string{backup, backup + ".manifest.json", backup + ".artifacts"} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	tmp := filepath.Join(directory, ".backup.db-123")
	if err := os.WriteFile(tmp, []byte("staging"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, old, old); err != nil {
		t.Fatal(err)
	}
	tmpDir := filepath.Join(directory, ".backup.db.artifacts-123")
	if err := os.MkdirAll(filepath.Join(tmpDir, "sha256"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmpDir, old, old); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runCommand(t, "backup", "cleanup", directory, "--older-than", "1h"); code != 0 || stderr != "" || !strings.Contains(stdout, "removed") {
		t.Fatalf("backup cleanup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("incomplete backup remains after cleanup: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("staging file remains after cleanup: %v", err)
	}
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Fatalf("staging directory remains after cleanup: %v", err)
	}
}

func TestBackupDefaultsAreProjectAwareIdempotentAndVerifiable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project with spaces")
	storePath := filepath.Join(root, ".missis-store", "missis.db")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := application.OpenPath(storePath)
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	staleManifest, err := client.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "fresh manifest"}); err != nil {
		t.Fatal(err)
	}
	manifest, err := client.Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if staleManifest.HeadHash == manifest.HeadHash {
		t.Fatal("fixture did not advance the live manifest")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MISSIS_STORE", storePath)
	t.Setenv("MISSIS_BACKUP_DIR", "")
	t.Setenv("MISSIS_MANIFEST_PATH", filepath.Join(t.TempDir(), "stale-manifest.json"))
	want := filepath.Join(root, ".missis-backups", strings.ReplaceAll(manifest.StoreID, ":", "_")+"-"+manifest.HeadHash+".db")
	code, stdout, stderr := runCommand(t, "backup")
	if code != 0 || stdout != want+"\n" || stderr != "" {
		t.Fatalf("default backup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, path := range []string{want, want + ".manifest.json", want + ".complete.json", want + ".artifacts"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("backup path %q: %v", path, err)
		}
	}
	code, stdout, stderr = runCommand(t, "backup")
	if code != 0 || !strings.Contains(stdout, "backup already exists:") || !strings.Contains(stdout, "verified current store") || stderr != "" {
		t.Fatalf("repeat backup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCommand(t, "backup", "verify")
	if code != 0 || stdout != "state=complete: backup verified against current store\n" || stderr != "" {
		t.Fatalf("implicit verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	relativeDir := "custom backups"
	t.Setenv("MISSIS_BACKUP_DIR", relativeDir)
	code, stdout, stderr = runCommand(t, "backup")
	relativeWant := filepath.Join(root, relativeDir, filepath.Base(want))
	if code != 0 || stdout != relativeWant+"\n" || stderr != "" {
		t.Fatalf("relative override: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	absDir := filepath.Join(t.TempDir(), "absolute backups")
	t.Setenv("MISSIS_BACKUP_DIR", absDir)
	code, stdout, stderr = runCommand(t, "backup")
	if code != 0 || stdout != filepath.Join(absDir, filepath.Base(want))+"\n" || stderr != "" {
		t.Fatalf("absolute override: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	t.Setenv("MISSIS_BACKUP_DIR", "")
	if err := os.WriteFile(want, []byte("conflicting backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := runCommand(t, "backup"); code == 0 || stdout != "" || !strings.Contains(stderr, "existing backup does not match current store") {
		t.Fatalf("conflicting default backup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestBackupAgainstCurrentRejectsDifferentStore(t *testing.T) {
	first := newTestStore(t)
	second := newTestStore(t)
	backup := filepath.Join(t.TempDir(), "explicit.db")
	t.Setenv("MISSIS_STORE", first)
	if code, stdout, stderr := runCommand(t, "backup", backup); code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("explicit backup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, _, stderr := runCommand(t, "backup", "verify", backup); code != 0 || stderr != "" {
		t.Fatalf("independent verify: code=%d stderr=%q", code, stderr)
	}
	t.Setenv("MISSIS_STORE", second)
	if code, stdout, stderr := runCommand(t, "backup", "verify", backup, "--against-current"); code == 0 || stdout != "" || stderr == "" {
		t.Fatalf("mismatched current verify: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func runCommand(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunHelpAndUnknownCommand(t *testing.T) {
	code, stdout, stderr := runCommand(t)
	if code != 2 || stdout != "" || !strings.Contains(stderr, "usage: missis-tools <command>") {
		t.Fatalf("missing command: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, stderr = runCommand(t, "--help")
	if code != 0 || !strings.Contains(stdout, "missis-tools") || stderr != "" {
		t.Fatalf("help: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, stderr = runCommand(t, "unknown")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "unknown command: unknown") {
		t.Fatalf("unknown: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestStoreMigrationCommandRequiresExplicitActionAndTarget(t *testing.T) {
	path := newTestStore(t)
	code, stdout, stderr := runCommand(t, "store", "migrate", "plan", "--store", path, "--to-format", "7", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("plan: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var plan store.MigrationPlan
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.FromFormat != 7 || plan.ToFormat != 7 || plan.RequiresBackup || plan.ChangesStoreID {
		t.Fatalf("plan = %#v", plan)
	}
	if code, _, stderr := runCommand(t, "store", "migrate", "plan", "--store", path); code != 2 || !strings.Contains(stderr, "--to-format") {
		t.Fatalf("missing target: code=%d stderr=%q", code, stderr)
	}
	if code, _, stderr := runCommand(t, "store", "migrate", "apply", "--store", path, "--to-format", "7"); code != 2 || !strings.Contains(stderr, "--backup") {
		t.Fatalf("missing backup: code=%d stderr=%q", code, stderr)
	}
}

func TestStoreForkCommandRequiresVersionSourceAndBackup(t *testing.T) {
	path := newTestStore(t)
	code, stdout, stderr := runCommand(t, "store", "fork", "plan", "--store", path, "--to-identity-version", "1", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("fork plan: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var plan store.WritableForkPlan
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Eligible || plan.FromStoreID == "" || plan.ToIdentityVersion != 1 {
		t.Fatalf("fork plan = %#v", plan)
	}
	if code, _, stderr := runCommand(t, "store", "fork", "plan", "--store", path); code != 2 || !strings.Contains(stderr, "--to-identity-version") {
		t.Fatalf("missing identity target: code=%d stderr=%q", code, stderr)
	}
	if code, _, stderr := runCommand(t, "store", "fork", "apply", "--store", path, "--to-identity-version", "1"); code != 2 || !strings.Contains(stderr, "--from-store-id") || !strings.Contains(stderr, "--backup") {
		t.Fatalf("missing fork confirmations: code=%d stderr=%q", code, stderr)
	}
	backup := filepath.Join(t.TempDir(), "pre-fork.db")
	code, stdout, stderr = runCommand(t, "store", "fork", "apply", "--store", path, "--to-identity-version", "1", "--from-store-id", plan.FromStoreID, "--backup", backup, "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("fork apply: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var report store.WritableForkReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "forked" || report.ToStoreID == plan.FromStoreID || report.ReceiptDigest == "" {
		t.Fatalf("fork report = %#v", report)
	}
}

func TestRunArgumentErrorsUseUmbrellaNames(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantOutput string
	}{
		{name: "repair missing", args: []string{"repair"}, wantOutput: "usage: missis-tools repair <missis.db>"},
		{name: "repair extra", args: []string{"repair", "one.db", "two.db"}, wantOutput: "usage: missis-tools repair <missis.db>"},
		{name: "gaps missing", args: []string{"gaps"}, wantOutput: "usage: missis-tools gaps <missis.db>"},
		{name: "backup extra", args: []string{"backup", "one.db", "two.db"}, wantOutput: "usage: missis-tools backup [destination]"},
		{name: "manifest extra", args: []string{"manifest", "one.db", "two.db"}, wantOutput: "usage: missis-tools manifest [missis.db]"},
		{name: "remote missing", args: []string{"remote"}, wantOutput: "usage: missis-tools remote <upload|download> [args]"},
		{name: "remote unknown", args: []string{"remote", "mirror"}, wantOutput: "unknown command: mirror"},
		{name: "remote upload extra", args: []string{"remote", "upload", "one.db", "two.db"}, wantOutput: "usage: missis-tools remote upload [source]"},
		{name: "remote download missing", args: []string{"remote", "download"}, wantOutput: "usage: missis-tools remote download <destination>"},
		{name: "remote download extra", args: []string{"remote", "download", "one.db", "two.db"}, wantOutput: "usage: missis-tools remote download <destination>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCommand(t, tt.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tt.wantOutput) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestRunStoreCommands(t *testing.T) {
	storePath := newTestStore(t)

	code, stdout, stderr := runCommand(t, "gaps", storePath)
	if code != 0 || stdout != "no sequence gaps\n" || stderr != "" {
		t.Fatalf("gaps: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, stderr = runCommand(t, "repair", storePath)
	if code != 0 || stdout != "store consistent; no sequence gaps\n" || stderr != "" {
		t.Fatalf("repair: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, stderr = runCommand(t, "manifest", storePath)
	if code != 0 || stderr != "" {
		t.Fatalf("manifest: code=%d stderr=%q", code, stderr)
	}
	var manifest missis.ManifestInfo
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, stdout)
	}
	if manifest.StoreID == "" {
		t.Fatalf("manifest missing store ID: %+v", manifest)
	}

	t.Setenv("MISSIS_STORE", storePath)
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	code, stdout, stderr = runCommand(t, "backup", backupPath)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("backup: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup not created: %v", err)
	}
}

func TestRunRemoteUploadAndDownload(t *testing.T) {
	storePath := newTestStore(t)
	t.Setenv("MISSIS_STORE", storePath)
	t.Setenv("MISSIS_REMOTE_DIR", filepath.Join(t.TempDir(), "remote"))

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if code := run([]string{"backup", backupPath}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("backup code=%d", code)
	}

	code, stdout, stderr := runCommand(t, "remote", "upload", backupPath)
	if code != 0 || !strings.HasPrefix(stdout, "uploaded ") || stderr != "" {
		t.Fatalf("upload: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	downloadPath := filepath.Join(t.TempDir(), "download.db")
	code, stdout, stderr = runCommand(t, "remote", "download", downloadPath)
	if code != 0 || stdout != "downloaded backup verified\n" || stderr != "" {
		t.Fatalf("download: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(downloadPath); err != nil {
		t.Fatalf("download not created: %v", err)
	}
}

func TestRunTUISmoke(t *testing.T) {
	t.Setenv("MISSIS_STORE", filepath.Join(t.TempDir(), "store.db"))
	var stdout, stderr bytes.Buffer
	code := run([]string{"tui", "--smoke"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "missis / tickets") || stderr.Len() != 0 {
		t.Fatalf("tui smoke: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTUIExplicitStoreAndOpenDiagnostics(t *testing.T) {
	storePath := newTestStore(t)
	envPath := filepath.Join(t.TempDir(), "env-store.db")
	t.Setenv("MISSIS_STORE", envPath)

	code, stdout, stderr := runCommand(t, "tui", "--store", storePath, "--smoke")
	if code != 0 || !strings.Contains(stdout, "missis / tickets") || stderr != "" {
		t.Fatalf("explicit store smoke: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("explicit --store should outrank MISSIS_STORE; env path stat error=%v", err)
	}

	badPath := filepath.Join(t.TempDir(), "store-directory")
	if err := os.Mkdir(badPath, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCommand(t, "tui", "--store", badPath, "--smoke")
	if code != 1 || !strings.Contains(stderr, "open missis store") || !strings.Contains(stderr, fmt.Sprintf("%q", badPath)) || !strings.Contains(stderr, "discovery=flag") || !strings.Contains(stderr, "runtime="+runtime.GOOS) || !strings.Contains(stderr, "hint:") || !strings.Contains(stderr, "missis-tools") {
		t.Fatalf("diagnostic open failure: code=%d stderr=%q", code, stderr)
	}
}
