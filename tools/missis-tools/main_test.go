package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/application"
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
