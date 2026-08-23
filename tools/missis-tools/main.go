package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/ravinsharma7/missis/internal/buildinfo"
	"github.com/ravinsharma7/missis/internal/tooling"
	"github.com/ravinsharma7/missis/internal/tui"
	"github.com/ravinsharma7/missis/internal/update"
)

const usage = "usage: missis-tools <command> [args]\n\n" +
	"commands:\n" +
	"  tui [--store PATH] [--smoke ...]  open the ticket TUI\n" +
	"  repair <missis.db>                verify consistency and report sequence gaps\n" +
	"  gaps <missis.db>                  report sequence gaps\n" +
	"  manifest [missis.db]              print the store manifest as JSON\n" +
	"  backup <destination>              create a consistent store backup\n" +
	"  backup verify <backup.db>         verify and classify a backup bundle\n" +
	"  backup cleanup <directory>        remove stale incomplete backup paths\n" +
	"  artifacts migrate [flags]         migrate legacy project-local artifacts offline\n" +
	"  artifacts gc [flags]              collect unindexed local artifacts offline\n" +
	"  remote upload [source]           upload a backup to the configured remote\n" +
	"  remote download <destination>    download and verify a backup\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, input io.Reader, stdout, stderr io.Writer) int {
	if input == nil {
		input = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if args[0] == "--complete-self-update" || args[0] == "--recover-self-update" {
		action := args[0]
		var binDir string
		var parentPID int
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--bin-dir":
				i++
				if i < len(args) {
					binDir = args[i]
				}
			case "--parent-pid":
				i++
				if i < len(args) {
					parentPID, _ = strconv.Atoi(args[i])
				}
			}
		}
		if binDir == "" || parentPID <= 0 {
			fmt.Fprintln(stderr, "invalid self-update completion request")
			return 2
		}
		var err error
		if action == "--complete-self-update" {
			err = update.CompleteWindowsUpdate(binDir, parentPID)
		} else {
			err = update.CompleteWindowsRecovery(binDir, parentPID)
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if err := update.RecoverCurrentInstallation(); err != nil {
		if errors.Is(err, update.ErrRecoveryStaged) {
			fmt.Fprintln(stderr, "interrupted update recovery staged; rerun after this process exits")
			return 1
		}
		fmt.Fprintf(stderr, "recover interrupted update: %v\n", err)
		return 1
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if args[0] == "--version" {
		info := buildinfo.Read()
		if len(args) > 1 && args[1] == "--json" {
			if err := json.NewEncoder(stdout).Encode(info); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		}
		fmt.Fprintf(stdout, "missis-tools version=%s commit=%s store_format=%d\n", info.DisplayVersion, info.Commit, info.StoreFormatRevision)
		return 0
	}

	switch args[0] {
	case "tui":
		return tui.Run(args[1:], input, stdout, stderr)
	case "repair":
		return tooling.RunRepairWithName(args[1:], stdout, stderr, "missis-tools repair")
	case "gaps":
		return tooling.RunGapsWithName(args[1:], stdout, stderr, "missis-tools gaps")
	case "manifest":
		if len(args) > 2 {
			fmt.Fprintln(stderr, "usage: missis-tools manifest [missis.db]")
			return 2
		}
		return tooling.RunManifest(args[1:], stdout, stderr)
	case "backup":
		return tooling.RunBackupWithName(args[1:], stdout, stderr, "missis-tools backup")
	case "artifacts":
		return tooling.RunArtifactsWithName(args[1:], stdout, stderr, "missis-tools artifacts")
	case "remote":
		return tooling.RunRemoteWithName(args[1:], stdout, stderr, "missis-tools remote")
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n\n%s", args[0], usage)
		return 2
	}
}
