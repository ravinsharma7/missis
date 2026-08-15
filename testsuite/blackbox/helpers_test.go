package blackbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var missisBin string

func TestMain(m *testing.M) {
	if env := os.Getenv("MISSIS_BIN"); env != "" {
		missisBin = env
		os.Exit(m.Run())
	}

	tmp, err := os.MkdirTemp("", "missis-bin-*")
	if err != nil {
		panic(err)
	}
	missisBin = filepath.Join(tmp, "missis")
	build := exec.Command("go", "build", "-o", missisBin, "github.com/ravinsharma7/missis/cmd/missis")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(tmp)
		panic(err)
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

func mustJSON(t *testing.T, result cmdResult) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &body); err != nil {
		t.Fatalf("json: %v\n%s", err, result.stdout)
	}
	return body
}
