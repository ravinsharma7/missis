package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/fsutil"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// RunArtifactsWithName implements offline local-artifact maintenance. It is
// deliberately separate from the three user-facing missis commands. The
// commands acquire an exclusive store lease and reject active clients.
func RunArtifactsWithName(args []string, stdout, stderr io.Writer, commandName string) int {
	stdout, stderr = commandWriters(stdout, stderr)
	if len(args) == 0 {
		fmt.Fprintf(stderr, "usage: %s <migrate|gc> [args] (offline; stop all missis clients first)\n", commandName)
		return 2
	}
	switch args[0] {
	case "migrate":
		return runArtifactMigration(args[1:], stdout, stderr, commandName+" migrate")
	case "gc":
		return runArtifactGC(args[1:], stdout, stderr, commandName+" gc")
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}

type artifactMaintenanceOptions struct {
	storePath    string
	artifactRoot string
	jsonOutput   bool
}

func parseArtifactMaintenanceFlags(args []string, commandName string) (artifactMaintenanceOptions, int, error) {
	var options artifactMaintenanceOptions
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.storePath, "store", "", "SQLite store path")
	flags.StringVar(&options.artifactRoot, "artifact-root", "", "artifact root override")
	flags.BoolVar(&options.jsonOutput, "json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return artifactMaintenanceOptions{}, 2, fmt.Errorf("usage: %s [--store PATH] [--artifact-root PATH] [--json]", commandName)
	}
	if flags.NArg() != 0 {
		return artifactMaintenanceOptions{}, 2, fmt.Errorf("usage: %s [--store PATH] [--artifact-root PATH] [--json]", commandName)
	}
	return options, 0, nil
}

type migrationReport struct {
	Status      string `json:"status"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Quarantine  string `json:"quarantine,omitempty"`
	Objects     int    `json:"objects"`
}

func runArtifactMigration(args []string, stdout, stderr io.Writer, commandName string) int {
	options, code, err := parseArtifactMaintenanceFlags(args, commandName)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return code
	}
	ctx := context.Background()
	resolved, err := missis.ResolveStore(options.storePath)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	lease, err := store.AcquireExclusiveLease(resolved.Path)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer lease.Close()
	sourceRoot := application.LegacyArtifactRoot(resolved.Path)
	info, err := os.Stat(sourceRoot)
	if errors.Is(err, os.ErrNotExist) {
		return writeMigrationReport(stdout, options.jsonOutput, migrationReport{Status: "not-needed", Source: sourceRoot})
	}
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if !info.IsDir() {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("legacy artifact root is not a directory: %s", sourceRoot))
	}
	if shaInfo, statErr := os.Stat(filepath.Join(sourceRoot, "sha256")); statErr != nil || !shaInfo.IsDir() {
		if statErr == nil {
			statErr = fmt.Errorf("sha256 entry is not a directory")
		}
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("legacy artifact root %q is not a valid CAS layout: %w", sourceRoot, statErr))
	}
	// OpenWithLease transfers ownership of the database lease to db.
	db, err := store.OpenWithLease(resolved.Path, lease, nil)
	if err != nil {
		_ = lease.Close()
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer db.Close()
	storeID, err := db.StoreIDContext(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	override := options.artifactRoot
	if strings.TrimSpace(override) == "" {
		override = os.Getenv("MISSIS_ARTIFACT_STORE")
	}
	destination, err := application.ArtifactRootForMaintenance(storeID, override)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if filepath.Clean(destination) == filepath.Clean(sourceRoot) {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("migration destination must differ from legacy root %s", sourceRoot))
	}
	sourceLease, err := store.AcquireExclusiveLease(sourceRoot)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer sourceLease.Close()
	destinationLease, err := store.AcquireExclusiveLease(destination)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer destinationLease.Close()
	source, err := artifact.NewLocalStore(sourceRoot)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	sourceObjects, err := source.Scan(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("scan legacy artifacts: %w", err))
	}
	for _, object := range sourceObjects {
		if !object.Valid {
			return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("legacy artifact %s is invalid: %w", object.Ref, object.Err))
		}
	}
	destinationStore, err := artifact.NewLocalStore(destination)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	destinationObjects, err := destinationStore.Scan(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("scan destination artifacts: %w", err))
	}
	for _, object := range destinationObjects {
		if !object.Valid {
			return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("destination artifact %s is invalid: %w", object.Ref, object.Err))
		}
	}
	for _, object := range sourceObjects {
		if err := copyLocalArtifact(ctx, source, destinationStore, object); err != nil {
			return printMaintenanceError(stderr, options.jsonOutput, err)
		}
	}
	records, err := db.ListArtifacts(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	for _, record := range records {
		ref, parseErr := artifact.ParseRef(record.Ref)
		if parseErr != nil {
			return printMaintenanceError(stderr, options.jsonOutput, parseErr)
		}
		metadata, statErr := destinationStore.Verify(ctx, ref)
		if statErr != nil {
			return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("indexed artifact %s is absent or invalid in destination: %w", record.Ref, statErr))
		}
		if metadata.Algorithm != record.Algorithm || metadata.Digest != record.Digest || metadata.MediaType != record.MediaType || metadata.Size != record.Size {
			return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("indexed artifact %s metadata does not match destination", record.Ref))
		}
	}
	quarantine, err := uniqueQuarantinePath(filepath.Dir(sourceRoot), filepath.Base(sourceRoot)+".legacy-")
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if err := os.Rename(sourceRoot, quarantine); err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("quarantine legacy artifact root: %w", err))
	}
	if err := syncMaintenanceDir(filepath.Dir(sourceRoot)); err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	return writeMigrationReport(stdout, options.jsonOutput, migrationReport{Status: "migrated", Source: sourceRoot, Destination: destination, Quarantine: quarantine, Objects: len(sourceObjects)})
}

func copyLocalArtifact(ctx context.Context, source, destination *artifact.LocalStore, object artifact.Object) error {
	reader, err := source.Open(ctx, object.Ref)
	if err != nil {
		return fmt.Errorf("open legacy artifact %s: %w", object.Ref, err)
	}
	metadata, putErr := destination.Put(ctx, reader, object.Metadata.MediaType)
	closeErr := reader.Close()
	if putErr != nil {
		return fmt.Errorf("copy legacy artifact %s: %w", object.Ref, putErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err := destination.Verify(ctx, object.Ref); err != nil {
		return fmt.Errorf("verify migrated artifact %s: %w", object.Ref, err)
	}
	if metadata != object.Metadata {
		return fmt.Errorf("migrated artifact %s metadata differs from source", object.Ref)
	}
	return nil
}

func uniqueQuarantinePath(dir, prefix string) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	base := filepath.Join(dir, prefix+stamp)
	path := base
	for i := 1; ; i++ {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		path = fmt.Sprintf("%s-%d", base, i)
	}
}

type gcReport struct {
	Status       string   `json:"status"`
	Root         string   `json:"root"`
	Grace        string   `json:"grace"`
	DryRun       bool     `json:"dry_run"`
	Deleted      []string `json:"deleted,omitempty"`
	Candidates   []string `json:"candidates,omitempty"`
	Invalid      []string `json:"invalid,omitempty"`
	StaleTemp    []string `json:"stale_temp,omitempty"`
	LegacyNotice string   `json:"legacy_notice,omitempty"`
}

func runArtifactGC(args []string, stdout, stderr io.Writer, commandName string) int {
	var options artifactMaintenanceOptions
	var graceText string
	var confirm bool
	var dryRun = true
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.storePath, "store", "", "SQLite store path")
	flags.StringVar(&options.artifactRoot, "artifact-root", "", "artifact root override")
	flags.StringVar(&graceText, "grace", "", "required grace duration")
	flags.BoolVar(&dryRun, "dry-run", true, "report without deleting")
	flags.BoolVar(&confirm, "confirm", false, "confirm deletion")
	flags.BoolVar(&options.jsonOutput, "json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintf(stderr, "usage: %s [--store PATH] [--artifact-root PATH] --grace DURATION [--dry-run|--confirm] [--json]\n", commandName)
		return 2
	}
	grace, err := time.ParseDuration(graceText)
	if err != nil || grace < 0 {
		fmt.Fprintf(stderr, "%s: --grace must be a non-negative duration\n", commandName)
		return 2
	}
	if confirm {
		dryRun = false
	}
	ctx := context.Background()
	resolved, err := missis.ResolveStore(options.storePath)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	lease, err := store.AcquireExclusiveLease(resolved.Path)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	db, err := store.OpenWithLease(resolved.Path, lease, nil)
	if err != nil {
		_ = lease.Close()
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer db.Close()
	storeID, err := db.StoreIDContext(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	override := options.artifactRoot
	if strings.TrimSpace(override) == "" {
		override = os.Getenv("MISSIS_ARTIFACT_STORE")
	}
	root, err := application.ArtifactRootForMaintenance(storeID, override)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if strings.HasPrefix(filepath.Base(root), "artifacts.legacy-") {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("refusing to garbage-collect quarantined legacy root: %s", root))
	}
	artifactLease, err := store.AcquireExclusiveLease(root)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer artifactLease.Close()
	legacyRoot := application.LegacyArtifactRoot(resolved.Path)
	if strings.TrimSpace(override) == "" {
		if legacyInfo, legacyErr := os.Stat(filepath.Join(legacyRoot, "sha256")); legacyErr == nil && legacyInfo.IsDir() {
			if _, newErr := os.Stat(root); errors.Is(newErr, os.ErrNotExist) {
				return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("legacy artifact root %q is still active; run artifacts migrate before gc", legacyRoot))
			}
		}
	}
	local, err := artifact.NewLocalStore(root)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	objects, err := local.Scan(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	records, err := db.ListArtifacts(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	live := make(map[string]struct{}, len(records))
	for _, record := range records {
		live[record.Ref] = struct{}{}
	}
	now := time.Now()
	report := gcReport{Status: "dry-run", Root: root, Grace: grace.String(), DryRun: dryRun}
	for _, object := range objects {
		if !object.Valid {
			report.Invalid = append(report.Invalid, gcObjectName(object))
			continue
		}
		if _, ok := live[object.Ref.String()]; ok {
			continue
		}
		if now.Sub(object.ModifiedAt) < grace {
			continue
		}
		report.Candidates = append(report.Candidates, object.Ref.String())
		if !dryRun {
			current, verifyErr := local.Verify(ctx, object.Ref)
			if verifyErr != nil {
				return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("recheck artifact %s before deletion: %w", object.Ref, verifyErr))
			}
			if _, ok := live[object.Ref.String()]; ok || current.Size != object.Metadata.Size {
				continue
			}
			if err := local.Remove(ctx, object.Ref); err != nil {
				return printMaintenanceError(stderr, options.jsonOutput, err)
			}
			report.Deleted = append(report.Deleted, object.Ref.String())
		}
	}
	report.StaleTemp, err = staleArtifactTemps(root, now, grace)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if !dryRun {
		for _, path := range report.StaleTemp {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return printMaintenanceError(stderr, options.jsonOutput, err)
			}
		}
		report.Status = "deleted"
	}
	return writeGCReport(stdout, options.jsonOutput, report)
}

func gcObjectName(object artifact.Object) string {
	if object.Ref != "" {
		return object.Ref.String() + ": " + object.Err.Error()
	}
	return object.DataPath + ": " + object.Err.Error()
}

func staleArtifactTemps(root string, now time.Time, grace time.Duration) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasPrefix(name, ".artifact-") && !strings.HasPrefix(name, ".metadata-") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) >= grace {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func writeMigrationReport(stdout io.Writer, jsonOutput bool, report migrationReport) int {
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			return 1
		}
		return 0
	}
	if report.Status == "not-needed" {
		fmt.Fprintln(stdout, "no legacy artifact root found")
		return 0
	}
	fmt.Fprintf(stdout, "migrated %d artifact objects to %s; legacy root quarantined at %s\n", report.Objects, report.Destination, report.Quarantine)
	return 0
}

func writeGCReport(stdout io.Writer, jsonOutput bool, report gcReport) int {
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "%s: %d candidates, %d invalid objects, %d stale temporary files\n", report.Status, len(report.Candidates), len(report.Invalid), len(report.StaleTemp))
	if len(report.Deleted) > 0 {
		fmt.Fprintf(stdout, "deleted %d orphaned objects\n", len(report.Deleted))
	}
	return 0
}

func printMaintenanceError(stderr io.Writer, jsonOutput bool, err error) int {
	if jsonOutput {
		_ = json.NewEncoder(stderr).Encode(map[string]any{
			"status": "error",
			"code":   store.MaintenanceErrorCode(err),
			"error":  err.Error(),
		})
	} else {
		fmt.Fprintln(stderr, err)
	}
	return 1
}

func syncMaintenanceDir(path string) error {
	return fsutil.SyncDir(path)
}
