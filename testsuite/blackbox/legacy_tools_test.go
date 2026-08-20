package blackbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLegacyToolWrappersAgainstTemporaryStore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.db")
	newTicket(t, storePath, "legacy wrapper fixture")

	build := func(tool string) string {
		t.Helper()
		name := tool
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(t.TempDir(), name)
		cmd := exec.Command("go", "build", "-o", path, "github.com/ravinsharma7/missis/tools/"+tool)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build legacy %s: %v\n%s", tool, err, output)
		}
		return path
	}

	run := func(binary string, env []string, args ...string) cmdResult {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), env...)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("run %s %v: %v", binary, args, err)
		}
		return cmdResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
	}

	storeEnv := []string{"MISSIS_STORE=" + storePath}
	gapsBinary := build("store-gaps")
	gaps := run(gapsBinary, nil, storePath)
	if gaps.code != 0 || gaps.stdout != "no sequence gaps\n" || gaps.stderr != "" {
		t.Fatalf("store-gaps: code=%d stdout=%q stderr=%q", gaps.code, gaps.stdout, gaps.stderr)
	}
	if usage := run(gapsBinary, nil); usage.code != 2 || !strings.Contains(usage.stderr, "usage: store-gaps <missis.db>") {
		t.Fatalf("store-gaps usage: code=%d stderr=%q", usage.code, usage.stderr)
	}

	repairBinary := build("repair-store")
	repair := run(repairBinary, nil, storePath)
	if repair.code != 0 || repair.stdout != "store consistent; no sequence gaps\n" || repair.stderr != "" {
		t.Fatalf("repair-store: code=%d stdout=%q stderr=%q", repair.code, repair.stdout, repair.stderr)
	}
	if usage := run(repairBinary, nil); usage.code != 2 || !strings.Contains(usage.stderr, "usage: repair-store <missis.db>") {
		t.Fatalf("repair-store usage: code=%d stderr=%q", usage.code, usage.stderr)
	}

	manifest := run(build("store-manifest"), nil, storePath)
	if manifest.code != 0 || !strings.Contains(manifest.stdout, "\"store_id\"") || manifest.stderr != "" {
		t.Fatalf("store-manifest: code=%d stdout=%q stderr=%q", manifest.code, manifest.stdout, manifest.stderr)
	}

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	backupBinary := build("store-backup")
	backup := run(backupBinary, storeEnv, backupPath)
	if backup.code != 0 || backup.stdout != "" || backup.stderr != "" {
		t.Fatalf("store-backup: code=%d stdout=%q stderr=%q", backup.code, backup.stdout, backup.stderr)
	}
	if usage := run(backupBinary, nil); usage.code != 1 || !strings.Contains(usage.stderr, "usage: store-backup <destination>") {
		t.Fatalf("store-backup usage: code=%d stderr=%q", usage.code, usage.stderr)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("store-backup output missing: %v", err)
	}

	remoteDir := filepath.Join(t.TempDir(), "remote")
	remoteEnv := append(storeEnv, "MISSIS_REMOTE_DIR="+remoteDir)
	remote := build("store-remote")
	if usage := run(remote, nil); usage.code != 2 || !strings.Contains(usage.stderr, "usage: store-remote <upload|download> [args]") {
		t.Fatalf("store-remote usage: code=%d stderr=%q", usage.code, usage.stderr)
	}
	upload := run(remote, remoteEnv, "upload", backupPath)
	if upload.code != 0 || !strings.HasPrefix(upload.stdout, "uploaded ") || upload.stderr != "" {
		t.Fatalf("store-remote upload: code=%d stdout=%q stderr=%q", upload.code, upload.stdout, upload.stderr)
	}
	downloadPath := filepath.Join(t.TempDir(), "download.db")
	download := run(remote, remoteEnv, "download", downloadPath)
	if download.code != 0 || download.stdout != "downloaded backup verified\n" || download.stderr != "" {
		t.Fatalf("store-remote download: code=%d stdout=%q stderr=%q", download.code, download.stdout, download.stderr)
	}
}
