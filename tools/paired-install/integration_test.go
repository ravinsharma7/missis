package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/ravinsharma7/missis/internal/update"
)

func TestPairedInstallSetsUpProjectAndRepeats(t *testing.T) {
	// covers PH1-SETUP-004 N125
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	staged, binDir := filepath.Join(tmp, "staged"), filepath.Join(tmp, "installed bin")
	project := filepath.Join(tmp, "project with spaces")
	for _, dir := range []string{staged, binDir, project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	version := "v9.9.9"
	commit := "0123456789abcdef0123456789abcdef01234567"
	ldflags := fmt.Sprintf("-X github.com/ravinsharma7/missis/internal/buildinfo.releaseVersion=%s -X github.com/ravinsharma7/missis/internal/buildinfo.releaseCommit=%s", version, commit)
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	build := func(output, pkg string) {
		t.Helper()
		cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", output, pkg)
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, data)
		}
	}
	missisPath := filepath.Join(staged, "missis"+suffix)
	toolsPath := filepath.Join(staged, "missis-tools"+suffix)
	installerPath := filepath.Join(tmp, "paired-install"+suffix)
	build(missisPath, "./cmd/missis")
	build(toolsPath, "./tools/missis-tools")
	build(installerPath, "./tools/paired-install")

	archive, format := releaseArchive(t, map[string]string{"missis" + suffix: missisPath, "missis-tools" + suffix: toolsPath})
	archiveHash := sha256.Sum256(archive)
	binaryHashes := map[string]string{"missis": fileHash(t, missisPath), "missis-tools": fileHash(t, toolsPath)}
	var manifestBytes []byte
	var manifestMu sync.RWMutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			manifestMu.RLock()
			body := append([]byte(nil), manifestBytes...)
			manifestMu.RUnlock()
			_, _ = w.Write(body)
		case "/bundle":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	manifest := update.ReleaseManifest{
		Version: version, Commit: commit, StoreFormatRevision: 2, PublishedAt: "2026-08-24T00:00:00Z",
		Assets: []update.Asset{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, URL: server.URL + "/bundle", Format: format,
			SHA256: hex.EncodeToString(archiveHash[:]), Size: int64(len(archive)), BinarySHA256: binaryHashes,
		}},
	}
	encodedManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestMu.Lock()
	manifestBytes = encodedManifest
	manifestMu.Unlock()

	for i := 0; i < 2; i++ {
		cmd := exec.Command(installerPath, "--ref", version, "--manifest-url", server.URL+"/manifest.json", "--bin-dir", binDir, "--project", project, "--json")
		cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "MISSIS_STORE=")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bootstrap %d: %v\n%s", i+1, err, output)
		}
		var result map[string]any
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("bootstrap JSON %d: %v\n%s", i+1, err, output)
		}
		if result["status"] != "ready" {
			t.Fatalf("bootstrap status %d = %v\n%s", i+1, result["status"], output)
		}
	}
	for _, path := range []string{filepath.Join(project, ".missis"), filepath.Join(project, ".missis-store", "missis.db")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("bootstrap artifact %s: %v", path, err)
		}
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func releaseArchive(t *testing.T, files map[string]string) ([]byte, string) {
	t.Helper()
	var buffer bytes.Buffer
	if runtime.GOOS == "windows" {
		writer := zip.NewWriter(&buffer)
		for name, path := range files {
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes(), "zip"
	}
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), "tar.gz"
}
