package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/ravinsharma7/missis/internal/buildinfo"
)

const (
	DefaultManifestURL = "https://github.com/ravinsharma7/missis/releases/latest/download/release-manifest.json"
	maxManifestSize    = 1 << 20
	maxArchiveSize     = 256 << 20
	InstallManifest    = ".missis-install.json"
)

var (
	ErrNoUpdate             = errors.New("no update available")
	ErrDevelopmentBuild     = errors.New("development or dirty builds cannot self-update")
	ErrUnpairedInstallation = errors.New("missis and missis-tools are not a registered paired installation")
	ErrUpdateStaged         = errors.New("update staged for completion after process exit")
	ErrRecoveryStaged       = errors.New("interrupted update recovery staged for completion after process exit")
)

const updateJournal = ".missis-update.json"

type replacementJournal struct {
	Staged       string       `json:"staged"`
	Installation Installation `json:"installation"`
}

type Asset struct {
	OS           string            `json:"os"`
	Arch         string            `json:"arch"`
	URL          string            `json:"url"`
	Format       string            `json:"format"`
	SHA256       string            `json:"sha256"`
	Size         int64             `json:"size"`
	BinarySHA256 map[string]string `json:"binary_sha256"`
}

type ReleaseManifest struct {
	Version               string  `json:"version"`
	Commit                string  `json:"commit"`
	StoreFormatRevision   int     `json:"store_format_revision"`
	NormalOpenFormat      int     `json:"normal_open_format"`
	MigratableFromFormats []int   `json:"migratable_from_formats"`
	MigrationSetDigest    string  `json:"migration_set_digest"`
	PublishedAt           string  `json:"published_at"`
	Assets                []Asset `json:"assets"`
}

type Installation struct {
	Version               string            `json:"version"`
	Commit                string            `json:"commit"`
	StoreFormatRevision   int               `json:"store_format_revision"`
	NormalOpenFormat      int               `json:"normal_open_format"`
	MigratableFromFormats []int             `json:"migratable_from_formats"`
	MigrationSetDigest    string            `json:"migration_set_digest"`
	Binaries              map[string]string `json:"binaries"`
}

type Client struct {
	ManifestURL string
	HTTP        *http.Client
	GOOS        string
	GOARCH      string
	Executable  string
}

type CheckResult struct {
	Current buildinfo.Info
	Latest  ReleaseManifest
	Asset   Asset
	Status  string
}

// PreparedInstallation is a verified paired release held outside the live
// binary names. Call Activate only after every store targeted by the same
// rollout has been migrated and verified.
type PreparedInstallation struct {
	Manifest     ReleaseManifest
	Installation Installation
	BinDir       string
	Staged       string
	GOOS         string
}

func (p *PreparedInstallation) ToolPath() string {
	name := "missis-tools"
	if p.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(p.Staged, name)
}

func (p *PreparedInstallation) Activate() error {
	if p == nil || p.BinDir == "" || p.Staged == "" {
		return fmt.Errorf("prepared installation is incomplete")
	}
	if err := replacePair(p.BinDir, p.Staged, p.Installation, p.GOOS); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Dir(p.Staged))
	return nil
}

func (p *PreparedInstallation) Discard() error {
	if p == nil || p.Staged == "" {
		return nil
	}
	return os.RemoveAll(filepath.Dir(p.Staged))
}

func DefaultClient() *Client {
	executable, _ := os.Executable()
	manifestURL := DefaultManifestURL
	if override := os.Getenv("MISSIS_UPDATE_MANIFEST_URL"); override != "" {
		manifestURL = override
	}
	return &Client{
		ManifestURL: manifestURL,
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Executable:  executable,
	}
}

func (c *Client) Check(ctx context.Context, current buildinfo.Info) (CheckResult, error) {
	if !buildinfo.IsStable(current) {
		return CheckResult{}, ErrDevelopmentBuild
	}
	manifest, err := c.fetchManifest(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	asset, err := manifest.asset(c.GOOS, c.GOARCH)
	if err != nil {
		return CheckResult{}, err
	}
	result := CheckResult{Current: current, Latest: manifest, Asset: asset, Status: "update_available"}
	switch semver.Compare(current.Version, manifest.Version) {
	case 0:
		result.Status = "current"
	case 1:
		result.Status = "newer_than_published"
	}
	return result, nil
}

func (c *Client) Apply(ctx context.Context, current buildinfo.Info) (ReleaseManifest, error) {
	check, err := c.Check(ctx, current)
	if err != nil {
		return ReleaseManifest{}, err
	}
	if check.Status != "update_available" {
		return check.Latest, ErrNoUpdate
	}
	binDir, err := c.pairedBinDir(current)
	if err != nil {
		return ReleaseManifest{}, err
	}
	stage, err := os.MkdirTemp(binDir, ".missis-update-stage-")
	if err != nil {
		return ReleaseManifest{}, err
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stage)
		}
	}()
	archivePath := filepath.Join(stage, "bundle")
	if err := c.download(ctx, check.Asset, archivePath); err != nil {
		return ReleaseManifest{}, err
	}
	extracted := filepath.Join(stage, "extracted")
	if err := extractBundle(archivePath, extracted, check.Asset.Format); err != nil {
		return ReleaseManifest{}, err
	}
	if err := verifyStagedBinaries(extracted, check.Latest, check.Asset, c.GOOS); err != nil {
		return ReleaseManifest{}, err
	}
	installation := Installation{
		Version: check.Latest.Version, Commit: check.Latest.Commit,
		StoreFormatRevision:   check.Latest.StoreFormatRevision,
		NormalOpenFormat:      check.Latest.NormalOpenFormat,
		MigratableFromFormats: append([]int(nil), check.Latest.MigratableFromFormats...),
		MigrationSetDigest:    check.Latest.MigrationSetDigest,
		Binaries:              check.Asset.BinarySHA256,
	}
	if c.GOOS == "windows" {
		if err := stageWindowsCompletion(binDir, extracted, installation); err != nil {
			return ReleaseManifest{}, err
		}
		keepStage = true
		return check.Latest, ErrUpdateStaged
	}
	if err := replacePair(binDir, extracted, installation, c.GOOS); err != nil {
		return ReleaseManifest{}, err
	}
	return check.Latest, nil
}

// Install downloads and verifies one paired release into binDir. It is used
// by the repository installers, which run outside either destination binary
// and can therefore publish the pair directly on every supported platform.
func (c *Client) Install(ctx context.Context, requestedVersion, binDir string) (ReleaseManifest, error) {
	prepared, err := c.PrepareInstall(ctx, requestedVersion, binDir)
	if err != nil {
		return ReleaseManifest{}, err
	}
	if err := prepared.Activate(); err != nil {
		return ReleaseManifest{}, err
	}
	return prepared.Manifest, nil
}

// PrepareInstall downloads, verifies, and stages both release binaries but
// does not replace either live binary or write an installation manifest.
func (c *Client) PrepareInstall(ctx context.Context, requestedVersion, binDir string) (*PreparedInstallation, error) {
	manifest, err := c.fetchManifest(ctx)
	if err != nil {
		return nil, err
	}
	if requestedVersion != "" && requestedVersion != "latest" && manifest.Version != requestedVersion {
		return nil, fmt.Errorf("release manifest version %s does not match requested %s", manifest.Version, requestedVersion)
	}
	asset, err := manifest.asset(c.GOOS, c.GOARCH)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(binDir, ".missis-update-stage-")
	if err != nil {
		return nil, err
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stage)
		}
	}()
	archivePath := filepath.Join(stage, "bundle")
	if err := c.download(ctx, asset, archivePath); err != nil {
		return nil, err
	}
	extracted := filepath.Join(stage, "extracted")
	if err := extractBundle(archivePath, extracted, asset.Format); err != nil {
		return nil, err
	}
	if err := verifyStagedBinaries(extracted, manifest, asset, c.GOOS); err != nil {
		return nil, err
	}
	installation := installationFromRelease(manifest, asset.BinarySHA256)
	keepStage = true
	return &PreparedInstallation{
		Manifest: manifest, Installation: installation,
		BinDir: filepath.Clean(binDir), Staged: extracted, GOOS: c.GOOS,
	}, nil
}

func (c *Client) fetchManifest(ctx context.Context) (ReleaseManifest, error) {
	url := c.ManifestURL
	if url == "" {
		url = DefaultManifestURL
	}
	if err := requireHTTPSOrLoopback(url); err != nil {
		return ReleaseManifest{}, fmt.Errorf("release manifest URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ReleaseManifest{}, err
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ReleaseManifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ReleaseManifest{}, fmt.Errorf("release manifest HTTP status %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize+1))
	if err != nil {
		return ReleaseManifest{}, err
	}
	if len(raw) > maxManifestSize {
		return ReleaseManifest{}, fmt.Errorf("release manifest exceeds %d bytes", maxManifestSize)
	}
	var manifest ReleaseManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return ReleaseManifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return ReleaseManifest{}, err
	}
	return manifest, nil
}

func (m ReleaseManifest) Validate() error {
	if !semver.IsValid(m.Version) || semver.Prerelease(m.Version) != "" {
		return fmt.Errorf("release version %q is not stable SemVer", m.Version)
	}
	if len(m.Commit) < 7 || len(m.Commit) > 64 || !isHex(m.Commit) {
		return fmt.Errorf("release commit is invalid")
	}
	if m.StoreFormatRevision < 1 || m.NormalOpenFormat != m.StoreFormatRevision ||
		!validMigratableFormats(m.MigratableFromFormats, m.NormalOpenFormat) ||
		len(m.MigrationSetDigest) != 64 || !isHex(m.MigrationSetDigest) ||
		len(m.Assets) == 0 {
		return fmt.Errorf("release manifest has invalid store format or no assets")
	}
	if m.PublishedAt != "" {
		if _, err := time.Parse(time.RFC3339, m.PublishedAt); err != nil {
			return fmt.Errorf("release published_at is invalid: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, asset := range m.Assets {
		key := asset.OS + "/" + asset.Arch
		if seen[key] {
			return fmt.Errorf("duplicate release asset for %s", key)
		}
		seen[key] = true
		if asset.OS == "" || asset.Arch == "" || asset.URL == "" || (asset.Format != "tar.gz" && asset.Format != "zip") ||
			asset.Size <= 0 || asset.Size > maxArchiveSize || len(asset.SHA256) != 64 || !isHex(asset.SHA256) {
			return fmt.Errorf("invalid release asset for %s", key)
		}
		if err := requireHTTPSOrLoopback(asset.URL); err != nil {
			return fmt.Errorf("invalid release asset URL for %s: %w", key, err)
		}
		for _, name := range []string{"missis", "missis-tools"} {
			hash := asset.BinarySHA256[name]
			if len(hash) != 64 || !isHex(hash) {
				return fmt.Errorf("asset %s has invalid %s hash", key, name)
			}
		}
	}
	return nil
}

func validMigratableFormats(formats []int, normalOpen int) bool {
	if len(formats) == 0 {
		return false
	}
	previous := 0
	includesNormal := false
	for _, revision := range formats {
		if revision < 1 || revision <= previous || revision > normalOpen {
			return false
		}
		if revision == normalOpen {
			includesNormal = true
		}
		previous = revision
	}
	return includesNormal
}

func sameFormats(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func installationFromRelease(manifest ReleaseManifest, binaries map[string]string) Installation {
	return Installation{
		Version:               manifest.Version,
		Commit:                manifest.Commit,
		StoreFormatRevision:   manifest.StoreFormatRevision,
		NormalOpenFormat:      manifest.NormalOpenFormat,
		MigratableFromFormats: append([]int(nil), manifest.MigratableFromFormats...),
		MigrationSetDigest:    manifest.MigrationSetDigest,
		Binaries:              binaries,
	}
}

func (m ReleaseManifest) asset(goos, goarch string) (Asset, error) {
	for _, asset := range m.Assets {
		if asset.OS == goos && asset.Arch == goarch {
			return asset, nil
		}
	}
	return Asset{}, fmt.Errorf("release %s has no asset for %s/%s", m.Version, goos, goarch)
}

func (c *Client) download(ctx context.Context, asset Asset, path string) error {
	if err := requireHTTPSOrLoopback(asset.URL); err != nil {
		return fmt.Errorf("release bundle URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("release bundle HTTP status %s", resp.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(resp.Body, maxArchiveSize+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != asset.Size || written > maxArchiveSize {
		return fmt.Errorf("release bundle size %d does not match manifest %d", written, asset.Size)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != asset.SHA256 {
		return fmt.Errorf("release bundle checksum mismatch: got %s want %s", got, asset.SHA256)
	}
	return nil
}

func (c *Client) pairedBinDir(current buildinfo.Info) (string, error) {
	executable := c.Executable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return "", err
		}
	}
	binDir := filepath.Dir(executable)
	installation, err := ReadInstallation(filepath.Join(binDir, InstallManifest))
	if err != nil {
		return "", fmt.Errorf("%w: %v; reinstall both binaries into one directory", ErrUnpairedInstallation, err)
	}
	if installation.Version != current.Version || installation.Commit != current.Commit ||
		installation.StoreFormatRevision != current.StoreFormatRevision ||
		installation.NormalOpenFormat != current.NormalOpenFormat ||
		!sameFormats(installation.MigratableFromFormats, current.MigratableFromFormats) ||
		installation.MigrationSetDigest != current.MigrationSetDigest {
		return "", fmt.Errorf("%w: installation manifest identity does not match running missis", ErrUnpairedInstallation)
	}
	for _, name := range binaryNames(c.GOOS) {
		path := filepath.Join(binDir, name)
		got, err := fileSHA256(path)
		if err != nil {
			return "", fmt.Errorf("%w: %s: %v", ErrUnpairedInstallation, name, err)
		}
		key := strings.TrimSuffix(name, ".exe")
		if got != installation.Binaries[key] {
			return "", fmt.Errorf("%w: installed %s checksum differs from installation manifest", ErrUnpairedInstallation, name)
		}
	}
	toolName := "missis-tools"
	if c.GOOS == "windows" {
		toolName += ".exe"
	}
	toolInfo, err := readBinaryInfo(filepath.Join(binDir, toolName))
	if err != nil || toolInfo.Version != current.Version || toolInfo.Commit != current.Commit ||
		toolInfo.StoreFormatRevision != current.StoreFormatRevision ||
		toolInfo.NormalOpenFormat != current.NormalOpenFormat ||
		!sameFormats(toolInfo.MigratableFromFormats, current.MigratableFromFormats) ||
		toolInfo.MigrationSetDigest != current.MigrationSetDigest || toolInfo.Dirty {
		return "", fmt.Errorf("%w: installed missis-tools identity differs from running missis", ErrUnpairedInstallation)
	}
	return binDir, nil
}

// VerifyCurrentInstallation verifies the registered release pair containing
// the running missis executable without contacting the update service.
func VerifyCurrentInstallation(current buildinfo.Info) (string, Installation, error) {
	c := DefaultClient()
	binDir, err := c.pairedBinDir(current)
	if err != nil {
		return "", Installation{}, err
	}
	installation, err := ReadInstallation(filepath.Join(binDir, InstallManifest))
	if err != nil {
		return "", Installation{}, err
	}
	return binDir, installation, nil
}

// VerifyDevelopmentPair verifies the companion binary beside the running
// executable. It deliberately does not create an installation manifest or
// make a development build eligible for self-update.
func VerifyDevelopmentPair(current buildinfo.Info) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	binDir := filepath.Dir(executable)
	toolName := "missis-tools"
	if runtime.GOOS == "windows" {
		toolName += ".exe"
	}
	toolInfo, err := readBinaryInfo(filepath.Join(binDir, toolName))
	if err != nil {
		return "", fmt.Errorf("verify development companion: %w", err)
	}
	if toolInfo.Version != current.Version || toolInfo.Commit != current.Commit ||
		toolInfo.StoreFormatRevision != current.StoreFormatRevision ||
		toolInfo.NormalOpenFormat != current.NormalOpenFormat ||
		!sameFormats(toolInfo.MigratableFromFormats, current.MigratableFromFormats) ||
		toolInfo.MigrationSetDigest != current.MigrationSetDigest ||
		toolInfo.Dirty != current.Dirty {
		return "", fmt.Errorf("%w: development companion identity differs from running missis", ErrUnpairedInstallation)
	}
	return binDir, nil
}

func RegisterInstallation(binDir string, info buildinfo.Info, goos string) (Installation, error) {
	installation := Installation{
		Version: info.Version, Commit: info.Commit,
		StoreFormatRevision:   info.StoreFormatRevision,
		NormalOpenFormat:      info.NormalOpenFormat,
		MigratableFromFormats: append([]int(nil), info.MigratableFromFormats...),
		MigrationSetDigest:    info.MigrationSetDigest,
		Binaries:              map[string]string{},
	}
	for _, filename := range binaryNames(goos) {
		hash, err := fileSHA256(filepath.Join(binDir, filename))
		if err != nil {
			return Installation{}, err
		}
		installation.Binaries[strings.TrimSuffix(filename, ".exe")] = hash
	}
	path := filepath.Join(binDir, InstallManifest)
	if err := writeJSONAtomic(path, installation, 0o600); err != nil {
		return Installation{}, err
	}
	return installation, nil
}

// RegisterPairedInstallation verifies the companion tool reports the same
// release identity before making the pair eligible for self-update.
func RegisterPairedInstallation(binDir string, info buildinfo.Info, goos string) (Installation, error) {
	if !buildinfo.IsStable(info) || info.Commit == buildinfo.UnknownCommit {
		return Installation{}, fmt.Errorf("%w: paired registration requires a stable release with a full commit identity", ErrUnpairedInstallation)
	}
	toolName := "missis-tools"
	if goos == "windows" {
		toolName += ".exe"
	}
	toolInfo, err := readBinaryInfo(filepath.Join(binDir, toolName))
	if err != nil {
		return Installation{}, fmt.Errorf("verify missis-tools identity: %w", err)
	}
	if toolInfo.Version != info.Version || toolInfo.StoreFormatRevision != info.StoreFormatRevision ||
		toolInfo.NormalOpenFormat != info.NormalOpenFormat ||
		!sameFormats(toolInfo.MigratableFromFormats, info.MigratableFromFormats) ||
		toolInfo.MigrationSetDigest != info.MigrationSetDigest {
		return Installation{}, fmt.Errorf("%w: missis=%s/format-%d missis-tools=%s/format-%d", ErrUnpairedInstallation, info.Version, info.StoreFormatRevision, toolInfo.Version, toolInfo.StoreFormatRevision)
	}
	if info.Commit != buildinfo.UnknownCommit && toolInfo.Commit != buildinfo.UnknownCommit && info.Commit != toolInfo.Commit {
		return Installation{}, fmt.Errorf("%w: binary commits differ", ErrUnpairedInstallation)
	}
	return RegisterInstallation(binDir, info, goos)
}

func readBinaryInfo(path string) (buildinfo.Info, error) {
	output, err := exec.Command(path, "--version", "--json").Output()
	if err != nil {
		return buildinfo.Info{}, err
	}
	var info buildinfo.Info
	if err := json.Unmarshal(output, &info); err != nil {
		return buildinfo.Info{}, err
	}
	if info.Version == "" || info.StoreFormatRevision < 1 ||
		info.NormalOpenFormat != info.StoreFormatRevision ||
		!validMigratableFormats(info.MigratableFromFormats, info.NormalOpenFormat) ||
		len(info.MigrationSetDigest) != 64 || !isHex(info.MigrationSetDigest) {
		return buildinfo.Info{}, fmt.Errorf("binary returned incomplete build identity")
	}
	return info, nil
}

func ReadInstallation(path string) (Installation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Installation{}, err
	}
	var installation Installation
	if err := json.Unmarshal(raw, &installation); err != nil {
		return Installation{}, err
	}
	if installation.Version == "" || installation.StoreFormatRevision < 1 ||
		installation.NormalOpenFormat != installation.StoreFormatRevision ||
		!validMigratableFormats(installation.MigratableFromFormats, installation.NormalOpenFormat) ||
		len(installation.MigrationSetDigest) != 64 || !isHex(installation.MigrationSetDigest) ||
		len(installation.Binaries) != 2 {
		return Installation{}, fmt.Errorf("invalid installation manifest")
	}
	return installation, nil
}

func extractBundle(archivePath, destination, format string) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	switch format {
	case "tar.gz":
		return extractTarGzip(archivePath, destination)
	case "zip":
		return extractZip(archivePath, destination)
	default:
		return fmt.Errorf("unsupported bundle format %q", format)
	}
}

func extractTarGzip(path, destination string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("bundle contains non-regular entry %q", header.Name)
		}
		if err := writeArchiveFile(destination, header.Name, reader, header.Size); err != nil {
			return err
		}
	}
}

func extractZip(path, destination string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, entry := range reader.File {
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("bundle contains non-regular entry %q", entry.Name)
		}
		body, err := entry.Open()
		if err != nil {
			return err
		}
		err = writeArchiveFile(destination, entry.Name, body, int64(entry.UncompressedSize64))
		body.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeArchiveFile(destination, name string, body io.Reader, size int64) error {
	clean := filepath.Clean(name)
	if clean != filepath.Base(clean) || clean == "." || strings.Contains(clean, "..") || size < 0 || size > maxArchiveSize {
		return fmt.Errorf("unsafe release bundle entry %q", name)
	}
	allowed := clean == "missis" || clean == "missis-tools" || clean == "missis.exe" || clean == "missis-tools.exe"
	if !allowed {
		return fmt.Errorf("unexpected release bundle entry %q", name)
	}
	path := filepath.Join(destination, clean)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(f, io.LimitReader(body, size+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != size {
		return fmt.Errorf("bundle entry %q size %d does not match %d", name, written, size)
	}
	return nil
}

func verifyStagedBinaries(root string, release ReleaseManifest, asset Asset, goos string) error {
	for _, filename := range binaryNames(goos) {
		path := filepath.Join(root, filename)
		got, err := fileSHA256(path)
		if err != nil {
			return err
		}
		key := strings.TrimSuffix(filename, ".exe")
		if got != asset.BinarySHA256[key] {
			return fmt.Errorf("%s checksum mismatch", filename)
		}
		cmd := exec.Command(path, "--version", "--json")
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("inspect staged %s: %w", filename, err)
		}
		var info buildinfo.Info
		if err := json.Unmarshal(output, &info); err != nil {
			return fmt.Errorf("decode staged %s version: %w", filename, err)
		}
		if info.Version != release.Version || info.Commit != release.Commit ||
			info.StoreFormatRevision != release.StoreFormatRevision ||
			info.NormalOpenFormat != release.NormalOpenFormat ||
			!sameFormats(info.MigratableFromFormats, release.MigratableFromFormats) ||
			info.MigrationSetDigest != release.MigrationSetDigest {
			return fmt.Errorf("staged %s identity does not match release manifest", filename)
		}
	}
	return nil
}

func replacePair(binDir, staged string, installation Installation, goos string) error {
	journal := replacementJournal{Staged: staged, Installation: installation}
	if err := writeJSONAtomic(filepath.Join(binDir, updateJournal), journal, 0o600); err != nil {
		return err
	}
	names := binaryNames(goos)
	type replacement struct {
		live, next, backup string
		hadLive            bool
	}
	replacements := make([]replacement, 0, len(names))
	for _, name := range names {
		_, statErr := os.Stat(filepath.Join(binDir, name))
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		replacements = append(replacements, replacement{
			live: filepath.Join(binDir, name), next: filepath.Join(staged, name), backup: filepath.Join(binDir, "."+name+".previous"),
			hadLive: statErr == nil,
		})
	}
	for _, item := range replacements {
		_ = os.Remove(item.backup)
	}
	completed := 0
	rollback := func() {
		for i := completed - 1; i >= 0; i-- {
			_ = os.Remove(replacements[i].live)
			if replacements[i].hadLive {
				_ = os.Rename(replacements[i].backup, replacements[i].live)
			}
		}
	}
	for _, item := range replacements {
		if item.hadLive {
			if err := os.Rename(item.live, item.backup); err != nil {
				rollback()
				return err
			}
		}
		if err := os.Rename(item.next, item.live); err != nil {
			if item.hadLive {
				_ = os.Rename(item.backup, item.live)
			}
			rollback()
			return err
		}
		completed++
	}
	if err := writeJSONAtomic(filepath.Join(binDir, InstallManifest), installation, 0o600); err != nil {
		rollback()
		return err
	}
	for _, item := range replacements {
		_ = os.Remove(item.backup)
	}
	_ = os.Remove(filepath.Join(binDir, updateJournal))
	return nil
}

// Recover resolves an interrupted pair replacement. If both live binaries
// match the verified target, publication is finalized; otherwise backups are
// restored. No downloaded file is executed by this path.
func Recover(binDir, goos string) error {
	journalPath := filepath.Join(binDir, updateJournal)
	raw, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		cleanupHelpers(binDir)
		return nil
	}
	if err != nil {
		return err
	}
	var journal replacementJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return fmt.Errorf("invalid update journal: %w", err)
	}
	rel, err := filepath.Rel(binDir, journal.Staged)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("update journal staged path is outside installation directory")
	}
	allNew := true
	for _, filename := range binaryNames(goos) {
		got, hashErr := fileSHA256(filepath.Join(binDir, filename))
		if hashErr != nil || got != journal.Installation.Binaries[strings.TrimSuffix(filename, ".exe")] {
			allNew = false
			break
		}
	}
	if allNew {
		if err := writeJSONAtomic(filepath.Join(binDir, InstallManifest), journal.Installation, 0o600); err != nil {
			return err
		}
		for _, filename := range binaryNames(goos) {
			_ = os.Remove(filepath.Join(binDir, "."+filename+".previous"))
		}
	} else {
		for _, filename := range binaryNames(goos) {
			live := filepath.Join(binDir, filename)
			backup := filepath.Join(binDir, "."+filename+".previous")
			if _, statErr := os.Stat(backup); statErr == nil {
				_ = os.Remove(live)
				if err := os.Rename(backup, live); err != nil {
					return err
				}
			}
		}
	}
	_ = os.RemoveAll(filepath.Dir(journal.Staged))
	_ = os.Remove(journalPath)
	cleanupHelpers(binDir)
	return nil
}

func RecoverCurrentInstallation() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	binDir := filepath.Dir(executable)
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(filepath.Join(binDir, updateJournal)); errors.Is(err, os.ErrNotExist) {
			cleanupHelpers(binDir)
			return nil
		} else if err != nil {
			return err
		}
		if err := stageWindowsHelper(binDir, "--recover-self-update"); err != nil {
			return err
		}
		return ErrRecoveryStaged
	}
	return Recover(binDir, runtime.GOOS)
}

func stageWindowsCompletion(binDir, staged string, installation Installation) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("windows update completion requested on %s", runtime.GOOS)
	}
	if err := writeJSONAtomic(filepath.Join(binDir, updateJournal), replacementJournal{Staged: staged, Installation: installation}, 0o600); err != nil {
		return err
	}
	return stageWindowsHelper(binDir, "--complete-self-update")
}

func stageWindowsHelper(binDir, action string) error {
	raw, err := os.ReadFile(filepath.Join(binDir, updateJournal))
	if err != nil {
		return err
	}
	var journal replacementJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return err
	}
	helperSource := filepath.Join(journal.Staged, "missis-tools.exe")
	if got, err := fileSHA256(helperSource); err != nil || got != journal.Installation.Binaries["missis-tools"] {
		return fmt.Errorf("verified Windows update helper is unavailable")
	}
	helper, err := os.CreateTemp(binDir, ".missis-update-helper-*.exe")
	if err != nil {
		return err
	}
	helperPath := helper.Name()
	if err := helper.Close(); err != nil {
		return err
	}
	source, err := os.Open(helperSource)
	if err != nil {
		return err
	}
	destination, err := os.OpenFile(helperPath, os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		source.Close()
		return err
	}
	_, copyErr := io.Copy(destination, source)
	source.Close()
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	command := exec.Command(helperPath, action, "--bin-dir", binDir, "--parent-pid", strconv.Itoa(os.Getpid()))
	if err := command.Start(); err != nil {
		return err
	}
	return nil
}

func CompleteWindowsUpdate(binDir string, parentPID int) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("windows update helper cannot run on %s", runtime.GOOS)
	}
	if err := waitForProcessExit(parentPID); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(binDir, updateJournal))
	if err != nil {
		return err
	}
	var journal replacementJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return err
	}
	return replacePair(binDir, journal.Staged, journal.Installation, "windows")
}

func CompleteWindowsRecovery(binDir string, parentPID int) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("windows update recovery helper cannot run on %s", runtime.GOOS)
	}
	if err := waitForProcessExit(parentPID); err != nil {
		return err
	}
	return Recover(binDir, "windows")
}

func cleanupHelpers(binDir string) {
	matches, _ := filepath.Glob(filepath.Join(binDir, ".missis-update-helper-*.exe"))
	for _, match := range matches {
		_ = os.Remove(match)
	}
}

func binaryNames(goos string) []string {
	if goos == "windows" {
		return []string{"missis.exe", "missis-tools.exe"}
	}
	return []string{"missis", "missis-tools"}
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".missis-manifest-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func isHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func requireHTTPSOrLoopback(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme == "https" && parsed.Host != "" {
		return nil
	}
	host := parsed.Hostname()
	if parsed.Scheme == "http" && (host == "127.0.0.1" || host == "::1" || host == "localhost") {
		return nil
	}
	return fmt.Errorf("HTTPS is required")
}
