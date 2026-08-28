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
		fmt.Fprintf(stderr, "usage: %s <verify|rebuild-index-copy|migrate|gc> [args] (offline; stop all missis clients first)\n", commandName)
		return 2
	}
	switch args[0] {
	case "verify":
		return runArtifactVerify(args[1:], stdout, stderr, commandName+" verify")
	case "rebuild-index-copy":
		return runArtifactIndexRebuildCopy(args[1:], stdout, stderr, commandName+" rebuild-index-copy")
	case "migrate":
		return runArtifactMigration(args[1:], stdout, stderr, commandName+" migrate")
	case "gc":
		return runArtifactGC(args[1:], stdout, stderr, commandName+" gc")
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}

type artifactVerificationEntry struct {
	Ref           string   `json:"ref"`
	Status        string   `json:"status"`
	EventIDs      []string `json:"event_ids,omitempty"`
	Locations     []string `json:"locations,omitempty"`
	Indexed       bool     `json:"indexed"`
	ObjectPresent bool     `json:"object_present"`
	ObjectValid   bool     `json:"object_valid"`
	Issues        []string `json:"issues,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type artifactVerificationReport struct {
	Status          string                      `json:"status"`
	Root            string                      `json:"root"`
	ReplayComplete  bool                        `json:"replay_complete"`
	ReferencedCount int                         `json:"referenced_count"`
	UnmanagedCount  int                         `json:"unmanaged_reference_count"`
	IndexedCount    int                         `json:"indexed_count"`
	ObjectCount     int                         `json:"object_count"`
	Entries         []artifactVerificationEntry `json:"entries"`
	InvalidPaths    []string                    `json:"invalid_paths,omitempty"`
	StagingPaths    []string                    `json:"staging_paths,omitempty"`
}

func runArtifactVerify(args []string, stdout, stderr io.Writer, commandName string) int {
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
	db, err := store.OpenWithLease(resolved.Path, lease, nil)
	if err != nil {
		_ = lease.Close()
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer db.Close()
	usages, err := db.ListAcceptedArtifactReferences(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("replay accepted artifact references: %w", err))
	}
	records, err := db.ListArtifacts(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	artifactNamespace, err := db.ArtifactNamespaceContext(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	override := options.artifactRoot
	if strings.TrimSpace(override) == "" {
		override = os.Getenv("MISSIS_ARTIFACT_STORE")
	}
	root, err := application.ArtifactRootForMaintenance(artifactNamespace, override)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	artifactLease, err := store.AcquireExclusiveLease(root)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer artifactLease.Close()

	usageByRef := make(map[string]store.ArtifactReferenceUsage, len(usages))
	allRefs := make(map[string]struct{})
	var unmanagedCount int
	for _, usage := range usages {
		usageByRef[usage.Ref] = usage
		allRefs[usage.Ref] = struct{}{}
		if !usage.Managed {
			unmanagedCount++
		}
	}
	recordByRef := make(map[string]store.ArtifactRecord, len(records))
	for _, record := range records {
		recordByRef[record.Ref] = record
		allRefs[record.Ref] = struct{}{}
	}
	objectByRef := make(map[string]artifact.Object)
	var invalidPaths []string
	if local, openErr := artifact.OpenLocalStore(root); openErr == nil {
		objects, scanErr := local.Scan(ctx)
		if scanErr != nil {
			return printMaintenanceError(stderr, options.jsonOutput, scanErr)
		}
		for _, object := range objects {
			if object.Ref == "" {
				invalidPaths = append(invalidPaths, object.DataPath+": "+object.Err.Error())
				continue
			}
			objectByRef[object.Ref.String()] = object
			allRefs[object.Ref.String()] = struct{}{}
		}
	} else if !errors.Is(openErr, os.ErrNotExist) {
		return printMaintenanceError(stderr, options.jsonOutput, openErr)
	}

	refs := make([]string, 0, len(allRefs))
	for ref := range allRefs {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	report := artifactVerificationReport{
		Status: "verified", Root: root, ReplayComplete: true,
		ReferencedCount: len(usages), UnmanagedCount: unmanagedCount, IndexedCount: len(records), ObjectCount: len(objectByRef), InvalidPaths: invalidPaths,
	}
	if _, statErr := os.Stat(root); statErr == nil {
		report.StagingPaths, err = staleArtifactTemps(root, time.Now(), 0)
		if err != nil {
			return printMaintenanceError(stderr, options.jsonOutput, err)
		}
	}
	hasIntegrityFailure := len(invalidPaths) > 0
	for _, ref := range refs {
		usage, referenced := usageByRef[ref]
		record, indexed := recordByRef[ref]
		object, knownObject := objectByRef[ref]
		dataPresent := knownObject && object.DataPath != ""
		entry := artifactVerificationEntry{Ref: ref, Status: "healthy", Indexed: indexed, ObjectPresent: dataPresent, ObjectValid: knownObject && object.Valid}
		if referenced {
			entry.EventIDs, entry.Locations = usage.EventIDs, usage.Locations
		}
		if referenced && !usage.Managed {
			entry.Status = "unmanaged-reference"
			entry.Issues = append(entry.Issues, "unmanaged-non-cas-reference")
			report.Entries = append(report.Entries, entry)
			continue
		}
		if referenced && !indexed {
			entry.Issues = append(entry.Issues, "missing-index")
		}
		if referenced && !dataPresent {
			entry.Issues = append(entry.Issues, "missing-object")
		}
		if knownObject && !object.Valid {
			if object.DataPath == "" {
				// missing-object was already named above when accepted history
				// references it; indexed-only rows need the same exact diagnosis.
				if !referenced {
					entry.Issues = append(entry.Issues, "missing-object")
				}
			} else if object.MetadataPath == "" {
				entry.Issues = append(entry.Issues, "missing-metadata")
			} else {
				entry.Issues = append(entry.Issues, "corrupt-object")
			}
			entry.Error = object.Err.Error()
		}
		if indexed && !referenced {
			entry.Issues = append(entry.Issues, "indexed-without-accepted-reference")
		}
		if knownObject && object.Valid && indexed && (record.Algorithm != object.Metadata.Algorithm || record.Digest != object.Metadata.Digest || record.Size != object.Metadata.Size) {
			entry.Issues = append(entry.Issues, "index-object-metadata-mismatch")
		}
		if knownObject && object.Valid && !referenced && !indexed {
			entry.Status = "unreferenced-object"
		} else if len(entry.Issues) > 0 {
			entry.Status = "inconsistent"
			hasIntegrityFailure = true
		}
		report.Entries = append(report.Entries, entry)
	}
	if hasIntegrityFailure {
		report.Status = "inconsistent"
	} else if report.UnmanagedCount > 0 {
		report.Status = "verified-with-unmanaged-references"
	} else if len(report.StagingPaths) > 0 {
		report.Status = "verified-with-recoverable-staging"
	}
	if options.jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "status=%s referenced=%d unmanaged=%d indexed=%d objects=%d staging=%d root=%s\n", report.Status, report.ReferencedCount, report.UnmanagedCount, report.IndexedCount, report.ObjectCount, len(report.StagingPaths), report.Root)
		for _, entry := range report.Entries {
			if entry.Status != "healthy" {
				fmt.Fprintf(stdout, "%s %s events=%s issues=%s error=%q\n", entry.Status, entry.Ref, strings.Join(entry.EventIDs, ","), strings.Join(entry.Issues, ","), entry.Error)
			}
		}
	}
	if hasIntegrityFailure {
		return 1
	}
	return 0
}

type artifactIndexRebuildReport struct {
	Status              string `json:"status"`
	Source              string `json:"source"`
	Destination         string `json:"destination"`
	StoreID             string `json:"store_id"`
	HeadDigest          string `json:"head_digest"`
	SourceEventCount    int64  `json:"source_event_count"`
	ReferencedObjects   int    `json:"referenced_objects"`
	UnmanagedReferences int    `json:"unmanaged_references"`
	RebuiltIndexRows    int    `json:"rebuilt_index_rows"`
	SourceIndexRows     int    `json:"source_index_rows"`
}

// runArtifactIndexRebuildCopy creates a replacement candidate while leaving
// the source database untouched. The authoritative events are copied exactly;
// only the destination's artifacts table is rebuilt from typed event replay
// plus fully verified CAS bytes.
func runArtifactIndexRebuildCopy(args []string, stdout, stderr io.Writer, commandName string) int {
	var options artifactMaintenanceOptions
	var destination string
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.storePath, "store", "", "SQLite store path")
	flags.StringVar(&options.artifactRoot, "artifact-root", "", "artifact root override")
	flags.StringVar(&destination, "destination", "", "new rebuilt SQLite path")
	flags.BoolVar(&options.jsonOutput, "json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(destination) == "" {
		fmt.Fprintf(stderr, "usage: %s [--store PATH] [--artifact-root PATH] --destination NEW.db [--json]\n", commandName)
		return 2
	}
	ctx := context.Background()
	resolved, err := missis.ResolveStore(options.storePath)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	sourceAbs, err := filepath.Abs(resolved.Path)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	destinationAbs, err := filepath.Abs(filepath.Clean(destination))
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if sourceAbs == destinationAbs {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("rebuild destination must differ from source database"))
	}
	if _, err := os.Stat(destinationAbs); err == nil {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("rebuild destination already exists: %s", destinationAbs))
	} else if !errors.Is(err, os.ErrNotExist) {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	lease, err := store.AcquireExclusiveLease(sourceAbs)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	db, err := store.OpenWithLease(sourceAbs, lease, nil)
	if err != nil {
		_ = lease.Close()
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer db.Close()
	usages, err := db.ListAcceptedArtifactReferences(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("replay accepted artifact references: %w", err))
	}
	oldRecords, err := db.ListArtifacts(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	namespace, err := db.ArtifactNamespaceContext(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	override := options.artifactRoot
	if strings.TrimSpace(override) == "" {
		override = os.Getenv("MISSIS_ARTIFACT_STORE")
	}
	root, err := application.ArtifactRootForMaintenance(namespace, override)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	artifactLease, err := store.AcquireExclusiveLease(root)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer artifactLease.Close()
	local, err := artifact.OpenLocalStore(root)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	rebuilt := make([]store.ArtifactRecord, 0, len(usages))
	var unmanagedReferences int
	for _, usage := range usages {
		if !usage.Managed {
			unmanagedReferences++
			continue
		}
		ref, err := artifact.ParseRef(usage.Ref)
		if err != nil {
			return printMaintenanceError(stderr, options.jsonOutput, err)
		}
		metadata, err := local.Verify(ctx, ref)
		if err != nil {
			return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("cannot rebuild %s referenced by events %s: %w", usage.Ref, strings.Join(usage.EventIDs, ","), err))
		}
		rebuilt = append(rebuilt, store.ArtifactRecord{
			Ref: metadata.Ref.String(), Algorithm: metadata.Algorithm, Digest: metadata.Digest,
			MediaType: metadata.MediaType, Size: metadata.Size, Backend: "local", RecordedAt: time.Now().UTC(),
		})
	}
	storeID, err := db.StoreID()
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	head, err := db.HeadHashContext(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	eventCount, err := db.EventCountContext(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if err := os.MkdirAll(filepath.Dir(destinationAbs), 0o700); err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(destinationAbs), ".artifact-index-rebuild-*.db")
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	staging := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(staging)
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if err := os.Remove(staging); err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	defer os.Remove(staging)
	if err := db.BackupContext(ctx, staging); err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	rebuiltDB, err := store.Open(staging)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if err := rebuiltDB.RebuildArtifactIndexContext(ctx, rebuilt); err != nil {
		_ = rebuiltDB.Close()
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if err := rebuiltDB.CheckConsistency(); err != nil {
		_ = rebuiltDB.Close()
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if err := rebuiltDB.Close(); err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if err := os.Rename(staging, destinationAbs); err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	if err := syncMaintenanceDir(filepath.Dir(destinationAbs)); err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	report := artifactIndexRebuildReport{
		Status: "rebuilt-copy", Source: sourceAbs, Destination: destinationAbs, StoreID: storeID,
		HeadDigest: head, SourceEventCount: eventCount, ReferencedObjects: len(rebuilt), UnmanagedReferences: unmanagedReferences,
		RebuiltIndexRows: len(rebuilt), SourceIndexRows: len(oldRecords),
	}
	if options.jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "rebuilt artifact index in replacement copy %s: store_id=%s head=%s events=%d managed_references=%d unmanaged_references=%d rows=%d\n", destinationAbs, storeID, head, eventCount, len(rebuilt), unmanagedReferences, len(rebuilt))
	}
	return 0
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
	artifactNamespace, err := db.ArtifactNamespaceContext(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	override := options.artifactRoot
	if strings.TrimSpace(override) == "" {
		override = os.Getenv("MISSIS_ARTIFACT_STORE")
	}
	destination, err := application.ArtifactRootForMaintenance(artifactNamespace, override)
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
	artifactNamespace, err := db.ArtifactNamespaceContext(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, err)
	}
	override := options.artifactRoot
	if strings.TrimSpace(override) == "" {
		override = os.Getenv("MISSIS_ARTIFACT_STORE")
	}
	root, err := application.ArtifactRootForMaintenance(artifactNamespace, override)
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
	accepted, err := db.ListAcceptedArtifactReferences(ctx)
	if err != nil {
		return printMaintenanceError(stderr, options.jsonOutput, fmt.Errorf("replay accepted artifact references before gc: %w", err))
	}
	// Both accepted event semantics and the current index protect an object.
	// A missing/stale index must be reconciled explicitly; GC never guesses
	// that either side is disposable.
	live := make(map[string]struct{}, len(records)+len(accepted))
	for _, record := range records {
		live[record.Ref] = struct{}{}
	}
	for _, usage := range accepted {
		live[usage.Ref] = struct{}{}
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
