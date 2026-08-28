package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ravinsharma7/missis/internal/store"
)

const (
	StoreRolloutProtocol = "paired-store-rollout-v1"
	storeRolloutJournal  = ".missis-store-rollout-v1.json"
)

type RolloutPhase string

const (
	RolloutStaged           RolloutPhase = "staged"
	RolloutRollbackPrepared RolloutPhase = "rollback-prepared"
	RolloutQuiesced         RolloutPhase = "quiesced"
	RolloutBackupPrepared   RolloutPhase = "backup-prepared"
	RolloutMigrating        RolloutPhase = "migrating"
	RolloutMigrated         RolloutPhase = "migrated"
	RolloutVerified         RolloutPhase = "verified-staged"
	RolloutActivating       RolloutPhase = "activating-pair"
	RolloutActivated        RolloutPhase = "activated-pair"
)

type StoreRolloutPlan struct {
	Protocol                   string              `json:"protocol"`
	StorePath                  string              `json:"store_path"`
	BackupPath                 string              `json:"backup_path,omitempty"`
	BinDir                     string              `json:"bin_dir"`
	FromRelease                string              `json:"from_release,omitempty"`
	ToRelease                  string              `json:"to_release"`
	ToCommit                   string              `json:"to_commit"`
	NormalOpenFormat           int                 `json:"normal_open_format"`
	MigratableFromFormats      []int               `json:"migratable_from_formats"`
	MigrationSetDigest         string              `json:"migration_set_digest"`
	StoreMigration             store.MigrationPlan `json:"store_migration"`
	RequiresStoreMigration     bool                `json:"requires_store_migration"`
	RequiresExclusiveLease     bool                `json:"requires_exclusive_lease"`
	PreviousInstallationStatus string              `json:"previous_installation_status"`
	StoreBytes                 int64               `json:"store_bytes"`
	MinimumBackupBytes         int64               `json:"minimum_backup_bytes"`
	MinimumBinaryRollbackBytes int64               `json:"minimum_binary_rollback_bytes"`
}

type StoreRolloutInspection struct {
	Status        string           `json:"status"`
	Phase         RolloutPhase     `json:"phase,omitempty"`
	Plan          StoreRolloutPlan `json:"plan,omitempty"`
	RollbackDir   string           `json:"rollback_dir,omitempty"`
	StagedPresent bool             `json:"staged_present"`
	LiveTarget    bool             `json:"live_target"`
	StoreFormat   int              `json:"store_format,omitempty"`
	Recovery      string           `json:"recovery,omitempty"`
}

type storeRolloutState struct {
	Protocol     string                 `json:"protocol"`
	Phase        RolloutPhase           `json:"phase"`
	Plan         StoreRolloutPlan       `json:"plan"`
	Staged       string                 `json:"staged"`
	GOOS         string                 `json:"goos"`
	Installation Installation           `json:"installation"`
	RollbackDir  string                 `json:"rollback_dir,omitempty"`
	RollbackSHA  map[string]string      `json:"rollback_sha256,omitempty"`
	BackupSHA    string                 `json:"backup_sha256,omitempty"`
	Migration    *store.MigrationReport `json:"migration,omitempty"`
}

type StoreRolloutOptions struct {
	AfterPhase func(RolloutPhase) error
}

func PlanStoreRollout(prepared *PreparedInstallation, storePath, backupPath string) (StoreRolloutPlan, error) {
	if err := validatePreparedInstallation(prepared); err != nil {
		return StoreRolloutPlan{}, err
	}
	absStore, err := filepath.Abs(filepath.Clean(storePath))
	if err != nil {
		return StoreRolloutPlan{}, err
	}
	migration, err := store.PlanMigration(absStore, prepared.Manifest.NormalOpenFormat)
	if err != nil {
		return StoreRolloutPlan{}, err
	}
	if !containsFormat(prepared.Manifest.MigratableFromFormats, migration.FromFormat) {
		return StoreRolloutPlan{}, fmt.Errorf("release %s cannot migrate store format %d; declared sources=%v",
			prepared.Manifest.Version, migration.FromFormat, prepared.Manifest.MigratableFromFormats)
	}
	backup := strings.TrimSpace(backupPath)
	if migration.RequiresBackup && backup == "" {
		return StoreRolloutPlan{}, fmt.Errorf("--backup is required for store format %d to %d rollout", migration.FromFormat, migration.ToFormat)
	}
	if backup != "" {
		backup, err = filepath.Abs(filepath.Clean(backup))
		if err != nil {
			return StoreRolloutPlan{}, err
		}
	}
	storeInfo, err := os.Stat(absStore)
	if err != nil {
		return StoreRolloutPlan{}, err
	}
	rollbackBytes, err := installedGenerationBytes(prepared.BinDir, prepared.GOOS)
	if err != nil {
		return StoreRolloutPlan{}, err
	}
	fromRelease := ""
	previousStatus := "absent"
	if previous, readErr := ReadInstallation(filepath.Join(prepared.BinDir, InstallManifest)); readErr == nil {
		fromRelease, previousStatus = previous.Version, "verified-manifest"
	} else if !errors.Is(readErr, os.ErrNotExist) {
		previousStatus = "unreadable-or-older-manifest"
	}
	return StoreRolloutPlan{
		Protocol: StoreRolloutProtocol, StorePath: absStore, BackupPath: backup,
		BinDir: prepared.BinDir, FromRelease: fromRelease,
		ToRelease: prepared.Manifest.Version, ToCommit: prepared.Manifest.Commit,
		NormalOpenFormat:      prepared.Manifest.NormalOpenFormat,
		MigratableFromFormats: append([]int(nil), prepared.Manifest.MigratableFromFormats...),
		MigrationSetDigest:    prepared.Manifest.MigrationSetDigest,
		StoreMigration:        migration, RequiresStoreMigration: migration.RequiresBackup,
		RequiresExclusiveLease: true, PreviousInstallationStatus: previousStatus,
		StoreBytes: storeInfo.Size(),
		MinimumBackupBytes: func() int64 {
			if migration.RequiresBackup {
				return storeInfo.Size()
			}
			return 0
		}(),
		MinimumBinaryRollbackBytes: rollbackBytes,
	}, nil
}

func ApplyStoreRollout(ctx context.Context, prepared *PreparedInstallation, storePath, backupPath string, options StoreRolloutOptions) (StoreRolloutInspection, error) {
	plan, err := PlanStoreRollout(prepared, storePath, backupPath)
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	journalPath := filepath.Join(prepared.BinDir, storeRolloutJournal)
	if _, err := os.Stat(journalPath); err == nil {
		return StoreRolloutInspection{}, fmt.Errorf("store rollout journal already exists; inspect or recover it first")
	} else if !errors.Is(err, os.ErrNotExist) {
		return StoreRolloutInspection{}, err
	}
	state := storeRolloutState{
		Protocol: StoreRolloutProtocol, Phase: RolloutStaged, Plan: plan,
		Staged: prepared.Staged, GOOS: prepared.GOOS, Installation: prepared.Installation,
	}
	if err := writeRolloutState(journalPath, &state, options); err != nil {
		return StoreRolloutInspection{}, err
	}
	state.RollbackDir, state.RollbackSHA, err = captureRollbackGeneration(prepared.BinDir, prepared.GOOS)
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	state.Phase = RolloutRollbackPrepared
	if err := writeRolloutState(journalPath, &state, options); err != nil {
		return StoreRolloutInspection{}, err
	}
	lease, err := store.AcquireExclusiveLease(plan.StorePath)
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	defer lease.Close()
	replanned, err := store.PlanMigration(plan.StorePath, plan.NormalOpenFormat)
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	if replanned.FromFormat != plan.StoreMigration.FromFormat ||
		replanned.FromStoreID != plan.StoreMigration.FromStoreID ||
		replanned.SourceHeadDigest != plan.StoreMigration.SourceHeadDigest ||
		replanned.SourceEventCount != plan.StoreMigration.SourceEventCount ||
		replanned.ArtifactNamespace != plan.StoreMigration.ArtifactNamespace {
		return StoreRolloutInspection{}, fmt.Errorf(
			"store changed after rollout plan: format=%d store_id=%q head=%q events=%d artifact_namespace=%q",
			replanned.FromFormat, replanned.FromStoreID, replanned.SourceHeadDigest,
			replanned.SourceEventCount, replanned.ArtifactNamespace)
	}
	state.Phase = RolloutQuiesced
	if err := writeRolloutState(journalPath, &state, options); err != nil {
		return StoreRolloutInspection{}, err
	}
	if replanned.RequiresBackup {
		state.BackupSHA, err = store.PrepareMigrationBackupWithLease(ctx, plan.StorePath, plan.NormalOpenFormat, plan.BackupPath, lease)
		if err != nil {
			return StoreRolloutInspection{}, err
		}
		state.Phase = RolloutBackupPrepared
		if err := writeRolloutState(journalPath, &state, options); err != nil {
			return StoreRolloutInspection{}, err
		}
		state.Phase = RolloutMigrating
		if err := writeRolloutState(journalPath, &state, options); err != nil {
			return StoreRolloutInspection{}, err
		}
		report, err := store.ResumeMigrationWithLease(ctx, plan.StorePath, plan.NormalOpenFormat, plan.BackupPath, lease)
		if err != nil {
			return StoreRolloutInspection{}, err
		}
		state.Migration = &report
	}
	state.Phase = RolloutMigrated
	if err := writeRolloutState(journalPath, &state, options); err != nil {
		return StoreRolloutInspection{}, err
	}
	if err := verifyStagedStore(prepared, plan.StorePath); err != nil {
		return StoreRolloutInspection{}, err
	}
	state.Phase = RolloutVerified
	if err := writeRolloutState(journalPath, &state, options); err != nil {
		return StoreRolloutInspection{}, err
	}
	state.Phase = RolloutActivating
	if err := writeRolloutState(journalPath, &state, options); err != nil {
		return StoreRolloutInspection{}, err
	}
	if err := prepared.Activate(); err != nil {
		return StoreRolloutInspection{}, err
	}
	if err := lease.Close(); err != nil {
		return StoreRolloutInspection{}, err
	}
	if err := verifyLiveStore(prepared.BinDir, prepared.GOOS, plan.StorePath); err != nil {
		return StoreRolloutInspection{}, err
	}
	state.Phase = RolloutActivated
	if err := writeRolloutState(journalPath, &state, options); err != nil {
		return StoreRolloutInspection{}, err
	}
	inspection, err := InspectStoreRollout(prepared.BinDir)
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	if !inspection.LiveTarget || inspection.StoreFormat != plan.NormalOpenFormat {
		return inspection, fmt.Errorf("activated rollout generation did not verify")
	}
	if err := os.Remove(journalPath); err != nil {
		return inspection, err
	}
	inspection.Status, inspection.Recovery = "committed", "none"
	return inspection, nil
}

func InspectStoreRollout(binDir string) (StoreRolloutInspection, error) {
	journalPath := filepath.Join(filepath.Clean(binDir), storeRolloutJournal)
	raw, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return StoreRolloutInspection{Status: "none", Recovery: "none"}, nil
	}
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	var state storeRolloutState
	if err := json.Unmarshal(raw, &state); err != nil || state.Protocol != StoreRolloutProtocol {
		return StoreRolloutInspection{}, fmt.Errorf("invalid store rollout journal")
	}
	if err := validateRolloutPaths(filepath.Clean(binDir), state); err != nil {
		return StoreRolloutInspection{}, err
	}
	stagedPresent := pathExists(state.Staged)
	liveTarget := liveMatchesInstallation(state.Plan.BinDir, state.GOOS, state.Installation)
	backupErr := verifyJournalBackup(state)
	migration, migrationErr := store.PlanMigration(state.Plan.StorePath, state.Plan.NormalOpenFormat)
	storeFormat := 0
	if migrationErr == nil {
		storeFormat = migration.FromFormat
	}
	recovery := "inspect-required"
	switch {
	case backupErr != nil:
		recovery = "integrity-incident-backup-mismatch"
	case storeFormat == state.Plan.NormalOpenFormat && liveTarget:
		recovery = "finish-commit"
	case storeFormat == state.Plan.NormalOpenFormat && stagedPresent:
		recovery = "finish-activation"
	case storeFormat == state.Plan.StoreMigration.FromFormat && phaseBefore(state.Phase, RolloutMigrating):
		recovery = "discard-staged-old-generation"
	case storeFormat == state.Plan.StoreMigration.FromFormat && stagedPresent:
		recovery = "resume-migration"
	case migrationErr != nil:
		recovery = "integrity-incident-store-unreadable"
	default:
		recovery = "integrity-incident-generation-mismatch"
	}
	return StoreRolloutInspection{
		Status: "incomplete", Phase: state.Phase, Plan: state.Plan,
		RollbackDir: state.RollbackDir, StagedPresent: stagedPresent,
		LiveTarget: liveTarget, StoreFormat: storeFormat, Recovery: recovery,
	}, nil
}

func RecoverStoreRollout(ctx context.Context, binDir string) (StoreRolloutInspection, error) {
	binDir = filepath.Clean(binDir)
	journalPath := filepath.Join(binDir, storeRolloutJournal)
	raw, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return StoreRolloutInspection{Status: "none", Recovery: "none"}, nil
	}
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	var state storeRolloutState
	if err := json.Unmarshal(raw, &state); err != nil || state.Protocol != StoreRolloutProtocol {
		return StoreRolloutInspection{}, fmt.Errorf("invalid store rollout journal")
	}
	if err := validateRolloutPaths(binDir, state); err != nil {
		return StoreRolloutInspection{}, err
	}
	if err := verifyJournalBackup(state); err != nil {
		return StoreRolloutInspection{}, err
	}
	if err := Recover(binDir, state.GOOS); err != nil {
		return StoreRolloutInspection{}, fmt.Errorf("recover paired replacement: %w", err)
	}
	lease, err := store.AcquireExclusiveLease(state.Plan.StorePath)
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	defer lease.Close()
	migration, err := store.PlanMigration(state.Plan.StorePath, state.Plan.NormalOpenFormat)
	if err != nil {
		return StoreRolloutInspection{}, err
	}
	if migration.FromFormat != state.Plan.NormalOpenFormat {
		if phaseBefore(state.Phase, RolloutMigrating) {
			_ = os.RemoveAll(filepath.Dir(state.Staged))
			_ = os.RemoveAll(state.RollbackDir)
			if err := os.Remove(journalPath); err != nil {
				return StoreRolloutInspection{}, err
			}
			return StoreRolloutInspection{Status: "rolled-back", StoreFormat: migration.FromFormat, Recovery: "none"}, nil
		}
		if !pathExists(state.Staged) {
			return StoreRolloutInspection{}, fmt.Errorf("integrity incident: staged target pair is missing while store remains format %d", migration.FromFormat)
		}
		var report store.MigrationReport
		if pathExists(state.Plan.BackupPath) {
			report, err = store.ResumeMigrationWithLease(ctx, state.Plan.StorePath, state.Plan.NormalOpenFormat, state.Plan.BackupPath, lease)
		} else {
			report, err = store.ApplyMigrationWithLease(ctx, state.Plan.StorePath, state.Plan.NormalOpenFormat, state.Plan.BackupPath, lease)
		}
		if err != nil {
			return StoreRolloutInspection{}, err
		}
		state.Migration, state.Phase = &report, RolloutMigrated
		if err := writeJSONAtomic(journalPath, state, 0o600); err != nil {
			return StoreRolloutInspection{}, err
		}
	}
	if liveMatchesInstallation(binDir, state.GOOS, state.Installation) {
		if err := writeJSONAtomic(filepath.Join(binDir, InstallManifest), state.Installation, 0o600); err != nil {
			return StoreRolloutInspection{}, err
		}
	} else {
		prepared := &PreparedInstallation{
			Manifest:     releaseFromInstallation(state.Installation),
			Installation: state.Installation, BinDir: binDir, Staged: state.Staged, GOOS: state.GOOS,
		}
		if err := validatePreparedInstallation(prepared); err != nil {
			return StoreRolloutInspection{}, err
		}
		if err := verifyStagedStore(prepared, state.Plan.StorePath); err != nil {
			return StoreRolloutInspection{}, err
		}
		if err := prepared.Activate(); err != nil {
			return StoreRolloutInspection{}, err
		}
	}
	if err := lease.Close(); err != nil {
		return StoreRolloutInspection{}, err
	}
	if err := verifyLiveStore(binDir, state.GOOS, state.Plan.StorePath); err != nil {
		return StoreRolloutInspection{}, err
	}
	if err := os.Remove(journalPath); err != nil {
		return StoreRolloutInspection{}, err
	}
	return StoreRolloutInspection{
		Status: "committed", Plan: state.Plan, RollbackDir: state.RollbackDir,
		LiveTarget: true, StoreFormat: state.Plan.NormalOpenFormat, Recovery: "none",
	}, nil
}

func writeRolloutState(path string, state *storeRolloutState, options StoreRolloutOptions) error {
	if err := writeJSONAtomic(path, state, 0o600); err != nil {
		return err
	}
	if options.AfterPhase != nil {
		return options.AfterPhase(state.Phase)
	}
	return nil
}

func validatePreparedInstallation(prepared *PreparedInstallation) error {
	if prepared == nil {
		return fmt.Errorf("prepared installation is required")
	}
	if err := prepared.Manifest.Validate(); err != nil {
		return err
	}
	if prepared.Manifest.Version != prepared.Installation.Version ||
		prepared.Manifest.Commit != prepared.Installation.Commit ||
		prepared.Manifest.NormalOpenFormat != prepared.Installation.NormalOpenFormat ||
		prepared.Manifest.MigrationSetDigest != prepared.Installation.MigrationSetDigest ||
		!sameFormats(prepared.Manifest.MigratableFromFormats, prepared.Installation.MigratableFromFormats) {
		return fmt.Errorf("prepared release and installation identities differ")
	}
	rel, err := filepath.Rel(prepared.BinDir, prepared.Staged)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("prepared pair is outside installation directory")
	}
	asset := Asset{BinarySHA256: prepared.Installation.Binaries}
	if err := verifyStagedBinaries(prepared.Staged, prepared.Manifest, asset, prepared.GOOS); err != nil {
		return err
	}
	return nil
}

func verifyStagedStore(prepared *PreparedInstallation, storePath string) error {
	plan, err := store.PlanMigration(storePath, prepared.Manifest.NormalOpenFormat)
	if err != nil {
		return err
	}
	if plan.RequiresBackup || plan.FromFormat != prepared.Manifest.NormalOpenFormat {
		return fmt.Errorf("staged verification found store format %d, expected %d", plan.FromFormat, prepared.Manifest.NormalOpenFormat)
	}
	if _, err := store.InspectReadOnly(context.Background(), storePath); err != nil {
		return fmt.Errorf("target implementation cannot verify migrated store: %w", err)
	}
	return nil
}

func verifyLiveStore(binDir, goos, storePath string) error {
	name := "missis-tools"
	if goos == "windows" {
		name += ".exe"
	}
	output, err := exec.Command(filepath.Join(binDir, name), "manifest", storePath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("activated missis-tools cannot verify target store: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var manifest struct {
		StoreID string `json:"store_id"`
	}
	if err := json.Unmarshal(output, &manifest); err != nil || manifest.StoreID == "" {
		return fmt.Errorf("activated missis-tools returned an invalid store manifest")
	}
	return nil
}

func captureRollbackGeneration(binDir, goos string) (string, map[string]string, error) {
	dir, err := os.MkdirTemp(binDir, ".missis-rollout-rollback-")
	if err != nil {
		return "", nil, err
	}
	hashes := map[string]string{}
	present := 0
	for _, name := range binaryNames(goos) {
		source := filepath.Join(binDir, name)
		if !pathExists(source) {
			continue
		}
		present++
		destination := filepath.Join(dir, name)
		if err := copyRegularFile(source, destination, 0o700); err != nil {
			return "", nil, err
		}
		hashes[name], err = fileSHA256(destination)
		if err != nil {
			return "", nil, err
		}
	}
	if present == 1 {
		return "", nil, fmt.Errorf("cannot capture split previous binary pair")
	}
	manifest := filepath.Join(binDir, InstallManifest)
	if pathExists(manifest) {
		if err := copyRegularFile(manifest, filepath.Join(dir, InstallManifest), 0o600); err != nil {
			return "", nil, err
		}
		hashes[InstallManifest], err = fileSHA256(filepath.Join(dir, InstallManifest))
		if err != nil {
			return "", nil, err
		}
	}
	return dir, hashes, nil
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("rollout input is not a regular file: %s", source)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, mode)
}

func validateRolloutPaths(binDir string, state storeRolloutState) error {
	for label, path := range map[string]string{"staged": state.Staged, "rollback": state.RollbackDir} {
		if path == "" {
			continue
		}
		rel, err := filepath.Rel(binDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("rollout %s path is outside installation directory", label)
		}
	}
	return nil
}

func liveMatchesInstallation(binDir, goos string, installation Installation) bool {
	for _, name := range binaryNames(goos) {
		got, err := fileSHA256(filepath.Join(binDir, name))
		if err != nil || got != installation.Binaries[strings.TrimSuffix(name, ".exe")] {
			return false
		}
	}
	return true
}

func releaseFromInstallation(installation Installation) ReleaseManifest {
	return ReleaseManifest{
		Version: installation.Version, Commit: installation.Commit,
		StoreFormatRevision:   installation.StoreFormatRevision,
		NormalOpenFormat:      installation.NormalOpenFormat,
		MigratableFromFormats: append([]int(nil), installation.MigratableFromFormats...),
		MigrationSetDigest:    installation.MigrationSetDigest,
		Assets: []Asset{{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: "https://invalid.example/retained",
			Format: "tar.gz", SHA256: strings.Repeat("0", 64), Size: 1,
			BinarySHA256: installation.Binaries}},
	}
}

func containsFormat(formats []int, target int) bool {
	for _, format := range formats {
		if format == target {
			return true
		}
	}
	return false
}

func phaseBefore(got, boundary RolloutPhase) bool {
	order := map[RolloutPhase]int{
		RolloutStaged: 1, RolloutRollbackPrepared: 2, RolloutQuiesced: 3,
		RolloutBackupPrepared: 4, RolloutMigrating: 5, RolloutMigrated: 6,
		RolloutVerified: 7, RolloutActivating: 8, RolloutActivated: 9,
	}
	return order[got] < order[boundary]
}

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func installedGenerationBytes(binDir, goos string) (int64, error) {
	names := append(binaryNames(goos), InstallManifest)
	var total int64
	for _, name := range names {
		info, err := os.Stat(filepath.Join(binDir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("installed generation member is not a regular file: %s", name)
		}
		total += info.Size()
	}
	return total, nil
}

func verifyJournalBackup(state storeRolloutState) error {
	if state.BackupSHA == "" {
		if !phaseBefore(state.Phase, RolloutBackupPrepared) && state.Plan.RequiresStoreMigration {
			return fmt.Errorf("integrity incident: rollout phase %s has no bound backup digest", state.Phase)
		}
		return nil
	}
	got, err := fileSHA256(state.Plan.BackupPath)
	if err != nil {
		return fmt.Errorf("integrity incident: read bound migration backup %q: %w", state.Plan.BackupPath, err)
	}
	if got != state.BackupSHA {
		return fmt.Errorf("integrity incident: migration backup digest mismatch: got %s want %s", got, state.BackupSHA)
	}
	return nil
}
