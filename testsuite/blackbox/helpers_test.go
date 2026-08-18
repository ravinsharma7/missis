package blackbox

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var missisBin string
var repairBin string

func TestMain(m *testing.M) {
	if env := os.Getenv("MISSIS_BIN"); env != "" {
		missisBin = env
		repairBin = os.Getenv("MISSIS_REPAIR_BIN")
		os.Exit(m.Run())
	}

	tmp, err := os.MkdirTemp("", "missis-bin-*")
	if err != nil {
		panic(err)
	}
	binName := "missis"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	missisBin = filepath.Join(tmp, binName)
	build := exec.Command("go", "build", "-o", missisBin, "github.com/ravinsharma7/missis/cmd/missis")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(tmp)
		panic(err)
	}
	if env := os.Getenv("MISSIS_REPAIR_BIN"); env != "" {
		repairBin = env
	} else {
		repairName := "repair-store"
		if runtime.GOOS == "windows" {
			repairName += ".exe"
		}
		repairBin = filepath.Join(tmp, repairName)
		buildRepair := exec.Command("go", "build", "-o", repairBin, "github.com/ravinsharma7/missis/tools/repair-store")
		buildRepair.Stdout = os.Stdout
		buildRepair.Stderr = os.Stderr
		if err := buildRepair.Run(); err != nil {
			os.RemoveAll(tmp)
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

type cmdResult struct {
	stdout string
	stderr string
	code   int
}

func runMissis(t *testing.T, store string, args ...string) cmdResult {
	return runMissisWithEnv(t, store, "", nil, args...)
}

func runMissisWithEnv(t *testing.T, store, dir string, env []string, args ...string) cmdResult {
	t.Helper()
	cmdArgs := args
	if store != "" && len(args) > 0 {
		cmdArgs = append([]string{args[0], "--store", store}, args[1:]...)
	}
	cmd := exec.Command(missisBin, cmdArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmdEnv := os.Environ()
	if store != "" {
		cmdEnv = append(cmdEnv, "MISSIS_STORE="+store)
	}
	cmdEnv = append(cmdEnv, env...)
	cmd.Env = cmdEnv
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return cmdResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func runMissisRaw(store string, args ...string) (cmdResult, error) {
	cmdArgs := args
	if len(args) > 0 {
		cmdArgs = append([]string{args[0], "--store", store}, args[1:]...)
	}
	cmd := exec.Command(missisBin, cmdArgs...)
	cmd.Env = append(os.Environ(), "MISSIS_STORE="+store)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
		err = nil
	}
	return cmdResult{stdout: stdout.String(), stderr: stderr.String(), code: code}, err
}

func newTicket(t *testing.T, store, title string) map[string]any {
	t.Helper()
	result := runMissis(t, store, "new", "--json", title)
	if result.code != 0 {
		t.Fatalf("new failed: %d %s", result.code, result.stderr)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &body); err != nil {
		t.Fatalf("new json: %v\n%s", err, result.stdout)
	}
	return body
}

// preserveStoreOnFailure arranges for the store files to be copied into
// MISSIS_DIAG_DIR when the test fails, so flaky concurrency failures keep
// their evidence (ticket #65). It is a no-op when MISSIS_DIAG_DIR is unset.
func preserveStoreOnFailure(t *testing.T, storePath string) {
	t.Helper()
	base := os.Getenv("MISSIS_DIAG_DIR")
	if base == "" {
		return
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		dir, err := preserveStoreDump(base, t.Name(), storePath)
		if err != nil {
			t.Logf("preserve store dump: %v", err)
			return
		}
		t.Logf("store diagnostics preserved at %s", dir)
	})
}

// preserveStoreDump copies storePath (plus -wal and -shm) and a metadata.json
// into base/<name>-<timestamp>/. Missing sidecar files are skipped; the main
// store file is expected to exist.
func preserveStoreDump(base, name, storePath string) (string, error) {
	dir := filepath.Join(base, sanitizeTestName(name)+"-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := copyFileIfExists(storePath, filepath.Join(dir, "missis.db")); err != nil {
		return "", err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyFileIfExists(storePath+suffix, filepath.Join(dir, "missis.db"+suffix)); err != nil {
			return "", err
		}
	}
	meta := map[string]any{
		"test":          name,
		"go_version":    runtime.Version(),
		"goos":          runtime.GOOS,
		"goarch":        runtime.GOARCH,
		"gomaxprocs":    runtime.GOMAXPROCS(0),
		"github_run_id": os.Getenv("GITHUB_RUN_ID"),
		"github_sha":    os.Getenv("GITHUB_SHA"),
		"timestamp":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), raw, 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

func sanitizeTestName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '_'
		}
		return r
	}, name)
}

func copyFileIfExists(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// assertNoDuplicateEventIDs scans the store for duplicate event IDs — the
// cross-process ULID collision mode of ticket #65. The appends themselves
// would fail on the UNIQUE constraint, but this assertion surfaces the
// collision class directly with evidence, so regressions are caught even if
// storage constraints change.
func assertNoDuplicateEventIDs(t *testing.T, storePath string) {
	t.Helper()
	db, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatalf("open store for duplicate check: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, COUNT(*) FROM events GROUP BY id HAVING COUNT(*) > 1`)
	if err != nil {
		t.Fatalf("query duplicate event ids: %v", err)
	}
	defer rows.Close()
	var duplicates []string
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			t.Fatalf("scan duplicate id: %v", err)
		}
		duplicates = append(duplicates, fmt.Sprintf("%s(x%d)", id, count))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate duplicate ids: %v", err)
	}
	if len(duplicates) > 0 {
		t.Fatalf("duplicate event IDs in %s: %v", storePath, duplicates)
	}
}

func mustJSON(t *testing.T, result cmdResult) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, result.stdout)
	}
	return body
}
