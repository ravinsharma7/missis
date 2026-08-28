package tooling

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func RunStoreWithName(args []string, stdout, stderr io.Writer, commandName string) int {
	stdout, stderr = commandWriters(stdout, stderr)
	if len(args) > 0 && args[0] == "fork" {
		return runStoreFork(args[1:], stdout, stderr, commandName+" fork")
	}
	if len(args) < 2 || args[0] != "migrate" || (args[1] != "plan" && args[1] != "apply") {
		fmt.Fprintf(stderr, "usage: %s <migrate|fork> <plan|apply> [versioned options]\n", commandName)
		return 2
	}
	action := args[1]
	flags := flag.NewFlagSet(commandName+" migrate "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var storePath, backupPath, targetText string
	var jsonOutput bool
	flags.StringVar(&storePath, "store", "", "SQLite store path")
	flags.StringVar(&targetText, "to-format", "", "required target store format revision")
	flags.StringVar(&backupPath, "backup", "", "required pre-migration backup path for apply")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || targetText == "" {
		fmt.Fprintf(stderr, "usage: %s migrate %s --to-format N [--store PATH] [--backup PATH] [--json]\n", commandName, action)
		return 2
	}
	target, err := strconv.Atoi(targetText)
	if err != nil || target < 1 {
		fmt.Fprintln(stderr, "--to-format must be a positive integer")
		return 2
	}
	resolved, err := missis.ResolveStore(storePath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if action == "plan" {
		if backupPath != "" {
			fmt.Fprintln(stderr, "--backup is valid only for migrate apply")
			return 2
		}
		plan, err := store.PlanMigration(resolved.Path, target)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if jsonOutput {
			if err := json.NewEncoder(stdout).Encode(plan); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		} else {
			fmt.Fprintf(stdout, "path=%s from_format=%d to_format=%d from_store_id=%s identity_scheme=%s backup_required=%t changes_store_id=%t\n",
				plan.Path, plan.FromFormat, plan.ToFormat, plan.FromStoreID, plan.FromIdentityScheme, plan.RequiresBackup, plan.ChangesStoreID)
		}
		return 0
	}
	if backupPath == "" {
		fmt.Fprintln(stderr, "--backup is required for migrate apply")
		return 2
	}
	report, err := store.ApplyMigration(context.Background(), resolved.Path, target, backupPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "status=%s from_format=%d to_format=%d from_store_id=%s to_store_id=%s receipt=%s backup=%s\n",
			report.Status, report.FromFormat, report.ToFormat, report.FromStoreID, report.ToStoreID, report.ReceiptID, report.BackupPath)
	}
	return 0
}

func runStoreFork(args []string, stdout, stderr io.Writer, commandName string) int {
	if len(args) > 0 && args[0] == "inspect" {
		return runStoreForkInspect(args[1:], stdout, stderr, commandName+" inspect")
	}
	if len(args) < 1 || (args[0] != "plan" && args[0] != "apply" && args[0] != "recover") {
		fmt.Fprintf(stderr, "usage: %s <plan|apply|recover|inspect> [versioned options]\n", commandName)
		return 2
	}
	action := args[0]
	flags := flag.NewFlagSet(commandName+" "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var storePath, backupPath, targetText, fromStoreID, sourceArtifactRoot, destinationArtifactRoot string
	var jsonOutput bool
	flags.StringVar(&storePath, "store", "", "SQLite store path")
	flags.StringVar(&targetText, "to-identity-version", "", "required target identity version")
	flags.StringVar(&fromStoreID, "from-store-id", "", "required source identity confirmation for apply")
	flags.StringVar(&backupPath, "backup", "", "required pre-fork backup path for apply")
	flags.StringVar(&sourceArtifactRoot, "source-artifact-root", "", "source local artifact namespace; defaults from the current store identity")
	flags.StringVar(&destinationArtifactRoot, "destination-artifact-root", "", "new independent local artifact namespace")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || targetText == "" {
		fmt.Fprintf(stderr, "usage: %s %s --to-identity-version N [--store PATH] [--from-store-id ID] [--backup PATH] [--source-artifact-root PATH] [--destination-artifact-root PATH] [--json]\n", commandName, action)
		return 2
	}
	target, err := strconv.Atoi(targetText)
	if err != nil || target < 1 {
		fmt.Fprintln(stderr, "--to-identity-version must be a positive integer")
		return 2
	}
	resolved, err := missis.ResolveStore(storePath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if action == "plan" {
		if backupPath != "" || fromStoreID != "" || destinationArtifactRoot != "" {
			fmt.Fprintln(stderr, "--backup, --from-store-id, and --destination-artifact-root are valid only for fork apply/recover")
			return 2
		}
		plan, err := store.PlanWritableFork(resolved.Path, target)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if plan.ArtifactRecordCount > 0 || plan.ManagedCASReferenceOccurrences > 0 {
			if strings.TrimSpace(sourceArtifactRoot) == "" {
				override := strings.TrimSpace(os.Getenv("MISSIS_ARTIFACT_STORE"))
				sourceArtifactRoot, err = application.ArtifactRootForMaintenance(plan.SourceArtifactNamespace, override)
				if err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
			plan, err = store.InspectWritableForkArtifactInventory(context.Background(), plan, sourceArtifactRoot)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
		if jsonOutput {
			if err := json.NewEncoder(stdout).Encode(plan); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		} else {
			fmt.Fprintf(stdout, "path=%s from_store_id=%s to_identity_version=%d eligible=%t backup_required=%t requires_artifact_namespace_fork=%t artifact_fork_protocol=%s receipt_version=%s artifact_records=%d accepted_artifact_reference_events=%d managed_cas_reference_occurrences=%d unmanaged_source_reference_occurrences=%d missing_artifact_index_count=%d artifact_inventory_status=%s required_objects=%d excluded_objects=%d integrity_issues=%q blocked_reason=%q\n",
				plan.Path, plan.FromStoreID, plan.ToIdentityVersion, plan.Eligible, plan.RequiresBackup, plan.RequiresArtifactNamespaceFork, plan.ArtifactForkProtocol, plan.ReceiptVersion, plan.ArtifactRecordCount, plan.AcceptedArtifactReferenceEventCount, plan.ManagedCASReferenceOccurrences, plan.UnmanagedSourceReferenceOccurrences, plan.MissingArtifactIndexCount, plan.ArtifactInventoryStatus, plan.RequiredManagedObjectCount, plan.ExcludedSourceObjectCount, strings.Join(plan.ArtifactIntegrityIssues, ","), plan.BlockedReason)
		}
		return 0
	}
	if backupPath == "" || fromStoreID == "" {
		fmt.Fprintln(stderr, "--backup and --from-store-id are required for fork apply")
		return 2
	}
	plan, err := store.PlanWritableFork(resolved.Path, target)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if plan.RequiresArtifactNamespaceFork && (plan.ArtifactRecordCount > 0 || plan.ManagedCASReferenceOccurrences > 0) && strings.TrimSpace(sourceArtifactRoot) == "" {
		override := strings.TrimSpace(os.Getenv("MISSIS_ARTIFACT_STORE"))
		sourceArtifactRoot, err = application.ArtifactRootForMaintenance(plan.SourceArtifactNamespace, override)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	report, err := store.ApplyWritableForkWithOptions(context.Background(), resolved.Path, target, fromStoreID, backupPath, store.WritableForkOptions{
		SourceArtifactRoot: sourceArtifactRoot, DestinationArtifactRoot: destinationArtifactRoot,
		ExecutionMode: func() store.WritableForkExecutionMode {
			if action == "recover" {
				return store.WritableForkRecoverV1
			}
			return store.WritableForkApplyV1
		}(),
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "status=%s from_store_id=%s to_store_id=%s to_identity_version=%d receipt=%s backup=%s\n",
			report.Status, report.FromStoreID, report.ToStoreID, report.ToIdentityVersion, report.ReceiptID, report.BackupPath)
	}
	return 0
}

func runStoreForkInspect(args []string, stdout, stderr io.Writer, commandName string) int {
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var storePath, destinationArtifactRoot string
	var jsonOutput bool
	flags.StringVar(&storePath, "store", "", "SQLite store path")
	flags.StringVar(&destinationArtifactRoot, "destination-artifact-root", "", "published destination root or its expected path")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(destinationArtifactRoot) == "" {
		fmt.Fprintf(stderr, "usage: %s [--store PATH] --destination-artifact-root PATH [--json]\n", commandName)
		return 2
	}
	resolved, err := missis.ResolveStore(storePath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	report, err := store.InspectArtifactNamespaceFork(context.Background(), resolved.Path, destinationArtifactRoot)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "status=%s root=%s from_store_id=%s to_store_id=%s database_store_id=%s objects=%d bytes=%d unmanaged=%d excluded=%d issues=%q\n",
			report.Status, report.Root, report.FromStoreID, report.ToStoreID, report.DatabaseStoreID, report.CopiedObjectCount, report.CopiedByteCount, report.UnmanagedReferenceCount, report.ExcludedObjectCount, strings.Join(report.Issues, ","))
	}
	if report.Status == "integrity-failure" || report.Status == "identity-mismatch" || report.Status == "incomplete-without-operation" {
		return 1
	}
	return 0
}
