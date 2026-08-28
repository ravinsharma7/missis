package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/store"
)

func TestStoreRolloutRecoversActivationBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX script fixture")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	stage := filepath.Join(binDir, ".missis-update-stage-test", "extracted")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missis", "missis-tools"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("old-"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	compatibility := store.FormatCompatibility()
	versionJSON, err := json.Marshal(map[string]any{
		"version": "v9.9.9", "display_version": "v9.9.9+g0123456789ab",
		"commit": "0123456789abcdef0123456789abcdef01234567", "dirty": false,
		"store_format_revision":   compatibility.NormalOpenFormat,
		"normal_open_format":      compatibility.NormalOpenFormat,
		"migratable_from_formats": compatibility.MigratableFromFormats,
		"migration_set_digest":    compatibility.MigrationSetDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for _, name := range []string{"missis", "missis-tools"} {
		body := []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '%%s\\n' '%s'; else printf '%%s\\n' '{\"store_id\":\"store:test\"}'; fi\n", versionJSON))
		path := filepath.Join(stage, name)
		if err := os.WriteFile(path, body, 0o700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	manifest := ReleaseManifest{
		Version: "v9.9.9", Commit: "0123456789abcdef0123456789abcdef01234567",
		StoreFormatRevision:   compatibility.NormalOpenFormat,
		NormalOpenFormat:      compatibility.NormalOpenFormat,
		MigratableFromFormats: compatibility.MigratableFromFormats,
		MigrationSetDigest:    compatibility.MigrationSetDigest,
		Assets: []Asset{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, URL: "https://example.invalid/bundle",
			Format: "tar.gz", SHA256: strings.Repeat("0", 64), Size: 1,
			BinarySHA256: hashes,
		}},
	}
	prepared := &PreparedInstallation{
		Manifest: manifest, Installation: installationFromRelease(manifest, hashes),
		BinDir: binDir, Staged: stage, GOOS: runtime.GOOS,
	}
	storePath := filepath.Join(root, "project", "missis.db")
	s, err := store.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("terminate before pair activation")
	_, err = ApplyStoreRollout(context.Background(), prepared, storePath, "", StoreRolloutOptions{
		AfterPhase: func(phase RolloutPhase) error {
			if phase == RolloutActivating {
				return injected
			}
			return nil
		},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("apply error = %v", err)
	}
	inspection, err := InspectStoreRollout(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Recovery != "finish-activation" || !inspection.StagedPresent || inspection.LiveTarget {
		t.Fatalf("inspection = %#v", inspection)
	}
	recovered, err := RecoverStoreRollout(context.Background(), binDir)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "committed" || !recovered.LiveTarget || recovered.StoreFormat != compatibility.NormalOpenFormat {
		t.Fatalf("recovered = %#v", recovered)
	}
	installation, err := ReadInstallation(filepath.Join(binDir, InstallManifest))
	if err != nil {
		t.Fatal(err)
	}
	if installation.MigrationSetDigest != compatibility.MigrationSetDigest {
		t.Fatalf("installation compatibility = %#v", installation)
	}
	if _, err := os.Stat(filepath.Join(binDir, storeRolloutJournal)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollout journal remains: %v", err)
	}
}

func TestReleaseManifestRejectsUnboundMigrationCompatibility(t *testing.T) {
	valid := testManifest("https://example.invalid/bundle")
	for name, mutate := range map[string]func(*ReleaseManifest){
		"normal mismatch":  func(m *ReleaseManifest) { m.NormalOpenFormat++ },
		"unsorted sources": func(m *ReleaseManifest) { m.MigratableFromFormats = []int{2, 1} },
		"target absent":    func(m *ReleaseManifest) { m.MigratableFromFormats = []int{1} },
		"bad digest":       func(m *ReleaseManifest) { m.MigrationSetDigest = "bad" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.MigratableFromFormats = append([]int(nil), valid.MigratableFromFormats...)
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid compatibility claim accepted")
			}
		})
	}
}

func TestStoreRolloutMigratesBeforeActivatingPair(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX script fixture")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	prepared := preparedScriptPair(t, binDir)
	for _, name := range []string{"missis", "missis-tools"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("old-"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join("..", "store", "testdata", "compatibility", "revision-0003", "fixture.db")
	storePath := filepath.Join(root, "project", "missis.db")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backups", "pre-format6.db")
	result, err := ApplyStoreRollout(context.Background(), prepared, storePath, backup, StoreRolloutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "committed" || result.Plan.StoreMigration.FromFormat != 3 ||
		result.StoreFormat != store.CurrentStoreFormatRevision || !result.LiveTarget {
		t.Fatalf("result = %#v", result)
	}
	if info, err := os.Stat(backup); err != nil || info.Size() == 0 {
		t.Fatalf("backup info=%v err=%v", info, err)
	}
	plan, err := store.PlanMigration(storePath, store.CurrentStoreFormatRevision)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiresBackup || plan.FromFormat != store.CurrentStoreFormatRevision {
		t.Fatalf("post-rollout plan = %#v", plan)
	}
}

func TestStoreRolloutRecoversEveryDurablePhase(t *testing.T) {
	// covers PH1-REL-002 N126
	if runtime.GOOS == "windows" {
		t.Skip("POSIX script fixture")
	}
	phases := []RolloutPhase{
		RolloutStaged, RolloutRollbackPrepared, RolloutQuiesced,
		RolloutBackupPrepared, RolloutMigrating, RolloutMigrated, RolloutVerified,
		RolloutActivating, RolloutActivated,
	}
	for _, faultPhase := range phases {
		t.Run(string(faultPhase), func(t *testing.T) {
			root := t.TempDir()
			binDir := filepath.Join(root, "bin")
			prepared := preparedScriptPair(t, binDir)
			for _, name := range []string{"missis", "missis-tools"} {
				if err := os.WriteFile(filepath.Join(binDir, name), []byte("old-"+name), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			source := filepath.Join("..", "store", "testdata", "compatibility", "revision-0003", "fixture.db")
			storePath := filepath.Join(root, "project", "missis.db")
			if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(storePath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(root, "backups", "pre-format6.db")
			injected := fmt.Errorf("terminate after %s", faultPhase)
			_, err = ApplyStoreRollout(context.Background(), prepared, storePath, backup, StoreRolloutOptions{
				AfterPhase: func(phase RolloutPhase) error {
					if phase == faultPhase {
						return injected
					}
					return nil
				},
			})
			if !errors.Is(err, injected) {
				t.Fatalf("apply error = %v", err)
			}
			recovered, err := RecoverStoreRollout(context.Background(), binDir)
			if err != nil {
				t.Fatal(err)
			}
			if phaseBefore(faultPhase, RolloutMigrating) {
				if recovered.Status != "rolled-back" || recovered.StoreFormat != 3 {
					t.Fatalf("pre-migration recovery = %#v", recovered)
				}
				return
			}
			if recovered.Status != "committed" || recovered.StoreFormat != store.CurrentStoreFormatRevision || !recovered.LiveTarget {
				t.Fatalf("post-migration recovery = %#v", recovered)
			}
		})
	}
}

func TestStoreRolloutRejectsChangedBoundBackup(t *testing.T) {
	// covers PH1-REL-002 N126
	if runtime.GOOS == "windows" {
		t.Skip("POSIX script fixture")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	prepared := preparedScriptPair(t, binDir)
	for _, name := range []string{"missis", "missis-tools"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("old-"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "store", "testdata", "compatibility", "revision-0003", "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, "project", "missis.db")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backups", "pre-format6.db")
	injected := errors.New("stop after backup")
	_, err = ApplyStoreRollout(context.Background(), prepared, storePath, backup, StoreRolloutOptions{
		AfterPhase: func(phase RolloutPhase) error {
			if phase == RolloutBackupPrepared {
				return injected
			}
			return nil
		},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("apply error = %v", err)
	}
	file, err := os.OpenFile(backup, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("changed"), 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectStoreRollout(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Recovery != "integrity-incident-backup-mismatch" {
		t.Fatalf("inspection = %#v", inspection)
	}
	if _, err := RecoverStoreRollout(context.Background(), binDir); err == nil || !strings.Contains(err.Error(), "backup digest mismatch") {
		t.Fatalf("changed backup recovery error = %v", err)
	}
}

type rolloutSubprocessConfig struct {
	Prepared  PreparedInstallation `json:"prepared"`
	StorePath string               `json:"store_path"`
	Backup    string               `json:"backup"`
	Barrier   string               `json:"barrier"`
	Result    string               `json:"result"`
	Fault     RolloutPhase         `json:"fault"`
}

func TestStoreRolloutSurvivesAbruptProcessDeathAtEveryPhase(t *testing.T) {
	// covers PH1-REL-002 N126
	if runtime.GOOS == "windows" {
		t.Skip("POSIX subprocess kill fixture")
	}
	phases := []RolloutPhase{
		RolloutStaged, RolloutRollbackPrepared, RolloutQuiesced,
		RolloutBackupPrepared, RolloutMigrating, RolloutMigrated,
		RolloutVerified, RolloutActivating, RolloutActivated,
	}
	for _, faultPhase := range phases {
		t.Run(string(faultPhase), func(t *testing.T) {
			root := t.TempDir()
			binDir := filepath.Join(root, "bin")
			prepared := preparedScriptPair(t, binDir)
			for _, name := range []string{"missis", "missis-tools"} {
				if err := os.WriteFile(filepath.Join(binDir, name), []byte("old-"+name), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			source, err := os.ReadFile(filepath.Join("..", "store", "testdata", "compatibility", "revision-0003", "fixture.db"))
			if err != nil {
				t.Fatal(err)
			}
			storePath := filepath.Join(root, "project", "missis.db")
			if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(storePath, source, 0o600); err != nil {
				t.Fatal(err)
			}
			config := rolloutSubprocessConfig{
				Prepared: *prepared, StorePath: storePath,
				Backup:  filepath.Join(root, "backups", "pre-format6.db"),
				Barrier: filepath.Join(root, "fault.barrier"),
				Result:  filepath.Join(root, "recovery.json"), Fault: faultPhase,
			}
			configPath := filepath.Join(root, "worker.json")
			writeRolloutSubprocessConfig(t, configPath, config)
			runRolloutApplyUntilKilled(t, configPath, config.Barrier)
			runRolloutRecoveryProcess(t, configPath)
			raw, err := os.ReadFile(config.Result)
			if err != nil {
				t.Fatal(err)
			}
			var recovered StoreRolloutInspection
			if err := json.Unmarshal(raw, &recovered); err != nil {
				t.Fatal(err)
			}
			if phaseBefore(faultPhase, RolloutMigrating) {
				if recovered.Status != "rolled-back" || recovered.StoreFormat != 3 {
					t.Fatalf("pre-migration recovery = %#v", recovered)
				}
			} else if recovered.Status != "committed" ||
				recovered.StoreFormat != store.CurrentStoreFormatRevision || !recovered.LiveTarget {
				t.Fatalf("post-migration recovery = %#v", recovered)
			}
		})
	}
}

func TestStoreRolloutSubprocessWorker(t *testing.T) {
	configPath := os.Getenv("MISSIS_ROLLOUT_SUBPROCESS_CONFIG")
	if configPath == "" {
		t.Skip("subprocess worker")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config rolloutSubprocessConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	switch os.Getenv("MISSIS_ROLLOUT_SUBPROCESS_MODE") {
	case "apply":
		_, err := ApplyStoreRollout(context.Background(), &config.Prepared, config.StorePath, config.Backup, StoreRolloutOptions{
			AfterPhase: func(phase RolloutPhase) error {
				if phase != config.Fault {
					return nil
				}
				if err := os.WriteFile(config.Barrier, []byte(phase), 0o600); err != nil {
					return err
				}
				for {
					time.Sleep(time.Second)
				}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	case "recover":
		result, err := RecoverStoreRollout(context.Background(), config.Prepared.BinDir)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.Result, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown subprocess mode")
	}
}

func writeRolloutSubprocessConfig(t *testing.T, path string, config rolloutSubprocessConfig) {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runRolloutApplyUntilKilled(t *testing.T, configPath, barrier string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreRolloutSubprocessWorker$")
	cmd.Env = append(os.Environ(),
		"MISSIS_ROLLOUT_SUBPROCESS_CONFIG="+configPath,
		"MISSIS_ROLLOUT_SUBPROCESS_MODE=apply",
	)
	var output strings.Builder
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.NewTimer(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("worker exited before barrier: %v\n%s", err, output.String())
		case <-ticker.C:
			if _, err := os.Stat(barrier); err == nil {
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				<-done
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		case <-deadline.C:
			_ = cmd.Process.Kill()
			<-done
			t.Fatalf("worker did not reach barrier\n%s", output.String())
		}
	}
}

func runRolloutRecoveryProcess(t *testing.T, configPath string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreRolloutSubprocessWorker$")
	cmd.Env = append(os.Environ(),
		"MISSIS_ROLLOUT_SUBPROCESS_CONFIG="+configPath,
		"MISSIS_ROLLOUT_SUBPROCESS_MODE=recover",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("recovery process: %v\n%s", err, output)
	}
}

func preparedScriptPair(t *testing.T, binDir string) *PreparedInstallation {
	t.Helper()
	stage := filepath.Join(binDir, ".missis-update-stage-test", "extracted")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	compatibility := store.FormatCompatibility()
	commit := "0123456789abcdef0123456789abcdef01234567"
	versionJSON, err := json.Marshal(map[string]any{
		"version": "v9.9.9", "display_version": "v9.9.9+g0123456789ab",
		"commit": commit, "dirty": false,
		"store_format_revision":   compatibility.NormalOpenFormat,
		"normal_open_format":      compatibility.NormalOpenFormat,
		"migratable_from_formats": compatibility.MigratableFromFormats,
		"migration_set_digest":    compatibility.MigrationSetDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for _, name := range []string{"missis", "missis-tools"} {
		body := []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '%%s\\n' '%s'; else printf '%%s\\n' '{\"store_id\":\"store:test\"}'; fi\n", versionJSON))
		path := filepath.Join(stage, name)
		if err := os.WriteFile(path, body, 0o700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	manifest := ReleaseManifest{
		Version: "v9.9.9", Commit: commit,
		StoreFormatRevision:   compatibility.NormalOpenFormat,
		NormalOpenFormat:      compatibility.NormalOpenFormat,
		MigratableFromFormats: compatibility.MigratableFromFormats,
		MigrationSetDigest:    compatibility.MigrationSetDigest,
		Assets: []Asset{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, URL: "https://example.invalid/bundle",
			Format: "tar.gz", SHA256: strings.Repeat("0", 64), Size: 1,
			BinarySHA256: hashes,
		}},
	}
	return &PreparedInstallation{
		Manifest: manifest, Installation: installationFromRelease(manifest, hashes),
		BinDir: binDir, Staged: stage, GOOS: runtime.GOOS,
	}
}
