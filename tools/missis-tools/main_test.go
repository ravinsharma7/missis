package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestRunArgumentErrorsUseUmbrellaNames(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantOutput string
	}{
		{name: "repair missing", args: []string{"repair"}, wantOutput: "usage: missis-tools repair <missis.db>"},
		{name: "repair extra", args: []string{"repair", "one.db", "two.db"}, wantOutput: "usage: missis-tools repair <missis.db>"},
		{name: "gaps missing", args: []string{"gaps"}, wantOutput: "usage: missis-tools gaps <missis.db>"},
		{name: "backup missing", args: []string{"backup"}, wantOutput: "usage: missis-tools backup <destination>"},
		{name: "backup extra", args: []string{"backup", "one.db", "two.db"}, wantOutput: "usage: missis-tools backup <destination>"},
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
