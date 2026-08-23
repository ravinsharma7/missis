package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/buildinfo"
)

func testManifest(url string) ReleaseManifest {
	hash := hex.EncodeToString(make([]byte, 32))
	return ReleaseManifest{
		Version: "v0.2.2", Commit: "0123456789abcdef0123456789abcdef01234567", StoreFormatRevision: 2,
		Assets: []Asset{{OS: "linux", Arch: "amd64", URL: url, Format: "tar.gz", SHA256: hash, Size: 1,
			BinarySHA256: map[string]string{"missis": hash, "missis-tools": hash}}},
	}
}

func manifestServer(t *testing.T, manifest ReleaseManifest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(manifest); err != nil {
			t.Error(err)
		}
	}))
}

func TestCheckVersionOrderingAndDevelopmentGuard(t *testing.T) {
	// covers PH1-REL-001
	manifest := testManifest("https://example.invalid/bundle")
	server := manifestServer(t, manifest)
	defer server.Close()
	client := &Client{ManifestURL: server.URL, HTTP: server.Client(), GOOS: "linux", GOARCH: "amd64"}
	for _, tc := range []struct{ version, status string }{
		{"v0.2.1", "update_available"}, {"v0.2.2", "current"}, {"v0.2.3", "newer_than_published"},
	} {
		result, err := client.Check(context.Background(), buildinfo.Info{Version: tc.version, Commit: "abc", DisplayVersion: tc.version, StoreFormatRevision: 2})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != tc.status {
			t.Errorf("version %s status = %s, want %s", tc.version, result.Status, tc.status)
		}
	}
	if _, err := client.Check(context.Background(), buildinfo.Info{Version: "dev", Commit: "abc"}); !errors.Is(err, ErrDevelopmentBuild) {
		t.Fatalf("development check error = %v", err)
	}
	if _, err := client.Check(context.Background(), buildinfo.Info{Version: "v0.2.1", Dirty: true}); !errors.Is(err, ErrDevelopmentBuild) {
		t.Fatalf("dirty check error = %v", err)
	}
}

func TestReleaseManifestValidation(t *testing.T) {
	// covers PH1-REL-001
	valid := testManifest("https://example.invalid/bundle")
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Version = "dev"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid version accepted")
	}
	invalid = valid
	invalid.Assets[0].BinarySHA256["missis"] = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid binary checksum accepted")
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := []byte("bad")
	if err := tw.WriteHeader(&tar.Header{Name: "../missis", Mode: 0o700, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractBundle(path, filepath.Join(t.TempDir(), "out"), "tar.gz"); err == nil {
		t.Fatal("path traversal archive accepted")
	}
}

func TestDownloadRejectsTampering(t *testing.T) {
	// covers PH1-REL-001
	body := []byte("tampered")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer server.Close()
	asset := Asset{URL: server.URL, Size: int64(len(body)), SHA256: hex.EncodeToString(make([]byte, 32))}
	client := &Client{HTTP: server.Client()}
	if err := client.download(context.Background(), asset, filepath.Join(t.TempDir(), "bundle")); err == nil {
		t.Fatal("tampered bundle accepted")
	}
}

func TestUpdateTransportRequiresHTTPS(t *testing.T) {
	client := &Client{ManifestURL: "http://example.test/release-manifest.json", GOOS: "linux", GOARCH: "amd64"}
	_, err := client.Check(context.Background(), buildinfo.Info{Version: "v0.2.1"})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("insecure manifest URL error = %v", err)
	}
	asset := Asset{URL: "http://example.test/bundle"}
	if err := client.download(context.Background(), asset, filepath.Join(t.TempDir(), "bundle")); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("insecure bundle URL error = %v", err)
	}
}

func TestRegisterAndReplacePairedInstallation(t *testing.T) {
	binDir := t.TempDir()
	for _, name := range []string{"missis", "missis-tools"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("old-"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	oldInfo := buildinfo.Info{Version: "v0.2.1", Commit: "old", StoreFormatRevision: 2}
	if _, err := RegisterInstallation(binDir, oldInfo, "linux"); err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	newHashes := map[string]string{}
	for _, name := range []string{"missis", "missis-tools"} {
		path := filepath.Join(stage, name)
		if err := os.WriteFile(path, []byte("new-"+name), 0o700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte("new-" + name))
		newHashes[name] = hex.EncodeToString(sum[:])
	}
	installation := Installation{Version: "v0.2.2", Commit: "new", StoreFormatRevision: 2, Binaries: newHashes}
	if err := replacePair(binDir, stage, installation, "linux"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadInstallation(filepath.Join(binDir, InstallManifest))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "v0.2.2" {
		t.Fatalf("installed version = %s", got.Version)
	}
	for _, name := range []string{"missis", "missis-tools"} {
		raw, err := os.ReadFile(filepath.Join(binDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != "new-"+name {
			t.Fatalf("%s content = %q", name, raw)
		}
	}
}

func TestSplitInstallationIsRejected(t *testing.T) {
	binDir := t.TempDir()
	missisPath := filepath.Join(binDir, "missis")
	if err := os.WriteFile(missisPath, []byte("missis"), 0o700); err != nil {
		t.Fatal(err)
	}
	client := &Client{Executable: missisPath, GOOS: "linux"}
	_, err := client.pairedBinDir(buildinfo.Info{Version: "v0.2.2", StoreFormatRevision: 2})
	if !errors.Is(err, ErrUnpairedInstallation) {
		t.Fatalf("split installation error = %v", err)
	}
}

func TestUnverifiedStagedBinaryIsNeverExecuted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	root := t.TempDir()
	marker := filepath.Join(root, "executed")
	for _, name := range []string{"missis", "missis-tools"} {
		body := []byte("#!/bin/sh\ntouch '" + marker + "'\n")
		if err := os.WriteFile(filepath.Join(root, name), body, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	badHash := strings.Repeat("0", 64)
	release := ReleaseManifest{Version: "v0.2.2", Commit: "0123456789abcdef0123456789abcdef01234567", StoreFormatRevision: 2}
	asset := Asset{BinarySHA256: map[string]string{"missis": badHash, "missis-tools": badHash}}
	if err := verifyStagedBinaries(root, release, asset, "linux"); err == nil {
		t.Fatal("unverified staged binaries accepted")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unverified binary executed: %v", err)
	}
}

func TestWindowsNamedPairUsesSameJournaledReplacement(t *testing.T) {
	binDir := t.TempDir()
	stage := filepath.Join(binDir, ".missis-update-stage-windows", "extracted")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for _, filename := range []string{"missis.exe", "missis-tools.exe"} {
		if err := os.WriteFile(filepath.Join(binDir, filename), []byte("old-"+filename), 0o700); err != nil {
			t.Fatal(err)
		}
		body := []byte("new-" + filename)
		if err := os.WriteFile(filepath.Join(stage, filename), body, 0o700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		hashes[strings.TrimSuffix(filename, ".exe")] = hex.EncodeToString(sum[:])
	}
	installation := Installation{Version: "v0.2.2", Commit: "new", StoreFormatRevision: 2, Binaries: hashes}
	if err := replacePair(binDir, stage, installation, "windows"); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"missis.exe", "missis-tools.exe"} {
		body, err := os.ReadFile(filepath.Join(binDir, filename))
		if err != nil || string(body) != "new-"+filename {
			t.Fatalf("%s body=%q err=%v", filename, body, err)
		}
	}
}

func TestRecoverRollsBackInterruptedPair(t *testing.T) {
	// covers PH1-REL-001
	binDir := t.TempDir()
	stage := filepath.Join(binDir, ".missis-update-stage-test", "extracted")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	newHashes := map[string]string{}
	for _, name := range []string{"missis", "missis-tools"} {
		old := []byte("old-" + name)
		if err := os.WriteFile(filepath.Join(binDir, name), old, 0o700); err != nil {
			t.Fatal(err)
		}
		newBody := []byte("new-" + name)
		if err := os.WriteFile(filepath.Join(stage, name), newBody, 0o700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(newBody)
		newHashes[name] = hex.EncodeToString(sum[:])
	}
	installation := Installation{Version: "v0.2.2", Commit: "new", StoreFormatRevision: 2, Binaries: newHashes}
	if err := writeJSONAtomic(filepath.Join(binDir, updateJournal), replacementJournal{Staged: stage, Installation: installation}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(binDir, "missis"), filepath.Join(binDir, ".missis.previous")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "missis"), []byte("new-missis"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Recover(binDir, "linux"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missis", "missis-tools"} {
		raw, err := os.ReadFile(filepath.Join(binDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != "old-"+name {
			t.Fatalf("%s after recovery = %q", name, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(binDir, updateJournal)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("update journal remains: %v", err)
	}
}

func TestInstallPublishesVerifiedPairAndManifest(t *testing.T) {
	// covers PH1-REL-001
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	commit := "0123456789abcdef0123456789abcdef01234567"
	versionJSON := fmt.Sprintf(`{"version":"v0.2.2","display_version":"v0.2.2+g0123456789ab","commit":%q,"dirty":false,"store_format_revision":2}`, commit)
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	binaryHashes := map[string]string{}
	for _, name := range []string{"missis", "missis-tools"} {
		body := []byte("#!/bin/sh\nprintf '%s\\n' '" + versionJSON + "'\n")
		sum := sha256.Sum256(body)
		binaryHashes[name] = hex.EncodeToString(sum[:])
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o700, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archiveSum := sha256.Sum256(archive.Bytes())
	var manifest ReleaseManifest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bundle.tar.gz" {
			_, _ = w.Write(archive.Bytes())
			return
		}
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()
	manifest = ReleaseManifest{
		Version: "v0.2.2", Commit: commit, StoreFormatRevision: 2,
		Assets: []Asset{{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: server.URL + "/bundle.tar.gz", Format: "tar.gz", SHA256: hex.EncodeToString(archiveSum[:]), Size: int64(archive.Len()), BinarySHA256: binaryHashes}},
	}
	binDir := t.TempDir()
	client := &Client{ManifestURL: server.URL + "/release-manifest.json", HTTP: server.Client(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if _, err := client.Install(context.Background(), "v0.2.2", binDir); err != nil {
		t.Fatal(err)
	}
	installation, err := ReadInstallation(filepath.Join(binDir, InstallManifest))
	if err != nil {
		t.Fatal(err)
	}
	if installation.Version != "v0.2.2" || installation.Commit != commit {
		t.Fatalf("installation identity = %#v", installation)
	}
	for _, name := range []string{"missis", "missis-tools"} {
		if got, err := fileSHA256(filepath.Join(binDir, name)); err != nil || got != binaryHashes[name] {
			t.Fatalf("installed %s hash = %q, err=%v", name, got, err)
		}
	}
}
