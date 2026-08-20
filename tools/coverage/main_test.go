package main

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestScanAllIncludesInternalTests(t *testing.T) {
	lines, err := scanAll(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"internal/model/registry_test.go",
		"internal/model/bitemporal_test.go",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("scan output does not include %q", want)
		}
	}
}

func TestScanFileReportsOpenError(t *testing.T) {
	_, err := scanFile(filepath.Join(t.TempDir(), "missing_test.go"), "missing_test.go")
	if err == nil {
		t.Fatal("scanFile should report an open error")
	}
}

func TestScanFileReportsScannerError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long_test.go")
	contents := strings.Repeat("x", bufio.MaxScanTokenSize+1)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := scanFile(path, "long_test.go")
	if err == nil {
		t.Fatal("scanFile should report a scanner error")
	}
}

func TestRunRegistryVerification(t *testing.T) {
	root := repositoryRoot(t)
	t.Chdir(root)
	if err := run([]string{"--registry", filepath.Join(root, "specs", "requirements-registry.v3.json")}); err != nil {
		t.Fatal(err)
	}
}
