package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/ravinsharma7/missis/internal/buildinfo"
	"github.com/ravinsharma7/missis/internal/update"
)

func main() {
	ref := flag.String("ref", "", "stable release tag (required for a local checkout)")
	binDir := flag.String("bin-dir", "", "destination directory for both binaries")
	project := flag.String("project", "", "existing project directory to set up after installation")
	jsonMode := flag.Bool("json", false, "emit combined installation and setup JSON")
	manifestURL := flag.String("manifest-url", "", "release manifest URL (tests only)")
	flag.Parse()
	inferred := inferredRef()
	selectedRef, err := selectRef(*ref, inferred)
	if err != nil {
		fmt.Fprintf(os.Stderr, "paired-install: %v\n", err)
		os.Exit(2)
	}
	*ref = selectedRef
	if !semver.IsValid(*ref) || semver.Prerelease(*ref) != "" {
		fmt.Fprintf(os.Stderr, "paired-install: ref %q is not a stable release tag\n", *ref)
		os.Exit(2)
	}
	if *binDir == "" {
		var err error
		*binDir, err = defaultBinDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "paired-install: resolve install directory: %v\n", err)
			os.Exit(2)
		}
	}
	absBinDir, err := filepath.Abs(*binDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "paired-install: resolve install directory: %v\n", err)
		os.Exit(2)
	}
	*binDir = filepath.Clean(absBinDir)
	if !pathContains(*binDir) {
		fmt.Fprintf(os.Stderr, "paired-install: install directory is not on PATH: %s\n", *binDir)
		if runtime.GOOS == "windows" {
			fmt.Fprintf(os.Stderr, "set $env:Path = \"%s;$env:Path\" and rerun the same command\n", *binDir)
		} else {
			fmt.Fprintf(os.Stderr, "export PATH=\"%s:$PATH\" and rerun the same command\n", *binDir)
		}
		os.Exit(2)
	}
	if err := ensureWritableBinDir(*binDir); err != nil {
		fmt.Fprintf(os.Stderr, "paired-install: install directory is not writable: %v\n", err)
		os.Exit(2)
	}
	if *project != "" {
		absProject, projectErr := filepath.Abs(*project)
		if projectErr != nil {
			fmt.Fprintf(os.Stderr, "paired-install: resolve project: %v\n", projectErr)
			os.Exit(2)
		}
		info, statErr := os.Stat(absProject)
		if statErr != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "paired-install: project directory must already exist: %s\n", absProject)
			os.Exit(2)
		}
		*project = absProject
	}
	url := *manifestURL
	if url == "" {
		url = "https://github.com/ravinsharma7/missis/releases/download/" + *ref + "/release-manifest.json"
	}
	client := update.DefaultClient()
	client.ManifestURL = url
	client.GOOS = runtime.GOOS
	client.GOARCH = runtime.GOARCH
	manifest, err := client.Install(context.Background(), *ref, *binDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "paired-install: %v\n", err)
		os.Exit(1)
	}
	installation, err := update.ReadInstallation(filepath.Join(*binDir, update.InstallManifest))
	if err != nil {
		fmt.Fprintf(os.Stderr, "paired-install: read verified installation: %v\n", err)
		os.Exit(1)
	}
	if *project == "" {
		if *jsonMode {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "installed", "bin_dir": *binDir, "installation": installation})
		} else {
			fmt.Printf("installed %s commit=%s store_format=%d in %s\n", buildinfo.ReleaseDisplay(manifest.Version, manifest.Commit), manifest.Commit, manifest.StoreFormatRevision, *binDir)
		}
		return
	}
	binary := filepath.Join(*binDir, "missis")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	cmd := exec.Command(binary, "--setup", "--project", *project, "--json")
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	setupErr := cmd.Run()
	var setup json.RawMessage
	if json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		setup = append(setup, bytes.TrimSpace(stdout.Bytes())...)
	} else {
		setup, _ = json.Marshal(map[string]any{"status": "failed", "message": strings.TrimSpace(stdout.String())})
	}
	combinedStatus := "ready"
	if setupErr != nil {
		combinedStatus = "setup_failed"
	}
	combined := map[string]any{
		"status": combinedStatus, "bin_dir": *binDir, "installation": installation,
		"setup": setup,
	}
	if message := strings.TrimSpace(stderr.String()); message != "" {
		combined["setup_stderr"] = message
	}
	if *jsonMode {
		_ = json.NewEncoder(os.Stdout).Encode(combined)
	} else {
		fmt.Printf("installed %s in %s\n", buildinfo.ReleaseDisplay(manifest.Version, manifest.Commit), *binDir)
		fmt.Print(stdout.String())
		if stderr.Len() != 0 {
			fmt.Fprint(os.Stderr, stderr.String())
		}
	}
	if setupErr != nil {
		if exitErr, ok := setupErr.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}

func inferredRef() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	// A VCS-built checkout can report the nearest tag with +dirty appended.
	// That is not a published module identity and must not conflict with the
	// explicit ref used by checkout and release automation.
	if strings.Contains(info.Main.Version, "+dirty") {
		return ""
	}
	if semver.IsValid(info.Main.Version) && semver.Prerelease(info.Main.Version) == "" {
		return info.Main.Version
	}
	return ""
}

func selectRef(explicit, inferred string) (string, error) {
	if explicit == "" {
		return inferred, nil
	}
	if inferred != "" && inferred != explicit {
		return "", fmt.Errorf("explicit ref %q conflicts with module version %q", explicit, inferred)
	}
	return explicit, nil
}

func defaultBinDir() (string, error) {
	if value := os.Getenv("MISSIS_BIN_DIR"); value != "" {
		return value, nil
	}
	if value := os.Getenv("GOBIN"); value != "" {
		return value, nil
	}
	output, err := exec.Command("go", "env", "GOPATH").Output()
	if err != nil {
		return "", err
	}
	gopath := strings.TrimSpace(string(output))
	if gopath == "" {
		return "", fmt.Errorf("go env GOPATH is empty")
	}
	if list := filepath.SplitList(gopath); len(list) > 0 {
		gopath = list[0]
	}
	return filepath.Join(gopath, "bin"), nil
}

func pathContains(dir string) bool {
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry != "" && sameDir(entry, dir) {
			return true
		}
	}
	return false
}

func sameDir(a, b string) bool {
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	return aErr == nil && bErr == nil && filepath.Clean(aAbs) == filepath.Clean(bAbs)
}

func ensureWritableBinDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, ".missis-install-check-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}
