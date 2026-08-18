package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestTUISmoke builds the real TUI binary and runs it in --smoke mode against
// a fresh store, asserting it renders the list view and exits cleanly. This is
// the terminal-driver-level smoke that CI (including windows-latest) runs via
// go test ./...; model-level rendering assertions live in main_test.go. It
// deliberately avoids a real TTY because headless runners have none.
func TestTUISmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TUI binary smoke in -short mode")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "ticket-tui")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build tui: %v\n%s", err, out)
	}

	store := filepath.Join(tmp, "store.db")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--smoke")
	cmd.Env = append(os.Environ(), "MISSIS_STORE="+store)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tui smoke run: %v\nstdout=%s\nstderr=%s", err, preview(stdout.String()), stderr.String())
	}
	out := stdout.String()
	if out == "" {
		t.Fatalf("tui smoke produced no output; stderr=%s", stderr.String())
	}
	if !strings.Contains(out, "missis tickets") {
		t.Fatalf("tui smoke output missing list header; stdout=%s stderr=%s", preview(out), stderr.String())
	}
}

func preview(s string) string {
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}
