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

// TestTUISmokeScopeViews builds the real TUI binary, seeds a store with a
// project, a group, and a homed ticket, then renders the scope views in
// --smoke mode: entity lists, entity detail, create prompt, and link prompt.
func TestTUISmokeScopeViews(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TUI binary smoke in -short mode")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "ticket-tui")
	missisBin := filepath.Join(tmp, "missis")
	if runtime.GOOS == "windows" {
		bin += ".exe"
		missisBin += ".exe"
	}
	for _, target := range []struct{ path, pkg string }{
		{bin, "."},
		{missisBin, "github.com/ravinsharma7/missis/cmd/missis"},
	} {
		build := exec.Command("go", "build", "-o", target.path, target.pkg)
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", target.pkg, err, out)
		}
	}

	store := filepath.Join(tmp, "store.db")
	seed := func(args ...string) {
		t.Helper()
		cmdArgs := append([]string{"new", "--store", store}, args...)
		cmd := exec.Command(missisBin, cmdArgs...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("seed %v: %v\n%s", args, err, out)
		}
	}
	seed("new", "--kind", "project", "--id", "safedesign", "SafeDesign")
	seed("new", "--kind", "group", "--id", "security", "Security")
	seed("new", "--project", "safedesign", "Homed ticket")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	frames := []struct {
		args []string
		want string
	}{
		{[]string{"--smoke", "--view", "list", "--kind", "projects"}, "missis projects"},
		{[]string{"--smoke", "--view", "list", "--kind", "groups"}, "missis groups"},
		{[]string{"--smoke", "--view", "detail", "--kind", "projects"}, "project:safedesign"},
		{[]string{"--smoke", "--view", "detail"}, "#1"},
		{[]string{"--smoke", "--view", "create", "--kind", "projects"}, "Create (kind:id Title)"},
		{[]string{"--smoke", "--view", "link"}, "Link (add|retract"},
	}
	for _, frame := range frames {
		cmd := exec.CommandContext(ctx, bin, frame.args...)
		cmd.Env = append(os.Environ(), "MISSIS_STORE="+store)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("smoke %v: %v\nstdout=%s\nstderr=%s", frame.args, err, preview(stdout.String()), stderr.String())
		}
		if !strings.Contains(stdout.String(), frame.want) {
			t.Fatalf("smoke %v missing %q; stdout=%s stderr=%s", frame.args, frame.want, preview(stdout.String()), stderr.String())
		}
	}
}

func preview(s string) string {
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}
