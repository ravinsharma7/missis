package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ravinsharma7/missis/internal/onboarding"
)

func main() {
	check := flag.Bool("check", false, "fail when generated onboarding files are stale")
	flag.Parse()
	root, err := repositoryRoot()
	if err != nil {
		fail(err)
	}
	stale := false
	stale = syncFile(filepath.Join(root, "docs", "agent-setup.md"), []byte(onboarding.AgentSetup()), *check) || stale
	readmePath := filepath.Join(root, "README.md")
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		fail(err)
	}
	want, err := replaceReadme(raw)
	if err != nil {
		fail(err)
	}
	stale = syncFile(readmePath, want, *check) || stale
	if stale && *check {
		fmt.Fprintln(os.Stderr, "generated onboarding is stale; run: go run ./tools/generate-onboarding")
		os.Exit(1)
	}
}

func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root not found")
		}
		dir = parent
	}
}

func replaceReadme(raw []byte) ([]byte, error) {
	begin, end := onboarding.ReadmeMarkers()
	text := string(raw)
	if start := strings.Index(text, begin); start >= 0 {
		finish := strings.Index(text[start:], end)
		if finish < 0 {
			return nil, fmt.Errorf("README generated onboarding end marker is missing")
		}
		finish = start + finish + len(end)
		return []byte(text[:start] + onboarding.ReadmeSection() + strings.TrimPrefix(text[finish:], "\n")), nil
	}
	start := strings.Index(text, "# Install\n")
	finish := strings.Index(text, "## Deletion and retraction\n")
	if start < 0 || finish < 0 || finish <= start {
		return nil, fmt.Errorf("README onboarding replacement boundaries not found")
	}
	return []byte(text[:start] + onboarding.ReadmeSection() + "\n" + text[finish:]), nil
}

func syncFile(path string, want []byte, check bool) bool {
	got, err := os.ReadFile(path)
	if err == nil && bytes.Equal(got, want) {
		return false
	}
	if check {
		fmt.Fprintf(os.Stderr, "stale generated file: %s\n", path)
		return true
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		fail(err)
	}
	return false
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
