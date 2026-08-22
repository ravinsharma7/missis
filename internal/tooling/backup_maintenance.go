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
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func runBackupVerify(args []string, stdout, stderr io.Writer, commandName string) int {
	stdout, stderr = commandWriters(stdout, stderr)
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: %s <backup.db>\n", commandName)
		return 2
	}
	path := filepath.Clean(args[0])
	lease, err := store.AcquireSharedLease(path)
	if err != nil {
		fmt.Fprintf(stderr, "state=%s: %v\n", application.BackupStateCorrupt, err)
		return 1
	}
	defer lease.Close()
	state, stateErr := application.ClassifyBackup(path)
	if stateErr != nil {
		fmt.Fprintf(stderr, "state=%s: %v\n", application.BackupStateCorrupt, stateErr)
		return 1
	}
	if state == application.BackupStateIncomplete {
		cleanupCommand := strings.TrimSuffix(commandName, " verify") + " cleanup"
		fmt.Fprintf(stderr, "state=%s: completion marker or required sidecars are missing; inspect with %s %s --older-than DURATION\n", state, cleanupCommand, filepath.Dir(path))
		return 1
	}
	svc, err := application.OpenPath(path)
	if err != nil {
		fmt.Fprintf(stderr, "state=%s: %v\n", application.BackupStateCorrupt, err)
		return 1
	}
	client := missis.NewClient(svc)
	defer client.Close()
	manifest, err := client.Manifest(context.Background())
	if err == nil {
		err = client.VerifyRestore(context.Background(), path, manifest)
	}
	if err != nil {
		fmt.Fprintf(stderr, "state=%s: %v\n", application.BackupStateCorrupt, err)
		return 1
	}
	fmt.Fprintf(stdout, "state=%s: backup verified\n", state)
	return 0
}

func runBackupCleanup(args []string, stdout, stderr io.Writer, commandName string) int {
	stdout, stderr = commandWriters(stdout, stderr)
	// Accept the documented positional-first form as well as conventional
	// flag-first parsing; the standard flag package stops at the first path.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(args[1:], args[0])
	}
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var olderText string
	flags.StringVar(&olderText, "older-than", "", "age of stale staging files or incomplete bundles")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s <directory> --older-than DURATION\n", commandName)
		return 2
	}
	older, err := time.ParseDuration(olderText)
	if err != nil || older < 0 {
		fmt.Fprintln(stderr, "--older-than must be a non-negative duration")
		return 2
	}
	directory := filepath.Clean(flags.Arg(0))
	removed, err := cleanupBackupDirectory(directory, older, time.Now())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "removed %d incomplete backup/staging paths\n", len(removed))
	return 0
}

func cleanupBackupDirectory(directory string, older time.Duration, now time.Time) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if !isBackupTempName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if now.Sub(info.ModTime()) < older {
			continue
		}
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		removed = append(removed, path)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		manifestPath := path + ".manifest.json"
		manifest, readErr := readToolBackupManifest(manifestPath)
		if errors.Is(readErr, os.ErrNotExist) {
			// A database without sidecars is a supported legacy backup. It is
			// never removed by cleanup because it remains readable.
			continue
		}
		if readErr != nil || manifest.Version < missis.BackupManifestVersion {
			continue
		}
		if _, markerErr := os.Stat(path + ".complete.json"); markerErr == nil {
			// A published marker, even if later verification fails, is not an
			// explicitly incomplete staging bundle. Keep it for operator review.
			continue
		} else if !os.IsNotExist(markerErr) {
			return nil, markerErr
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if now.Sub(info.ModTime()) < older {
			continue
		}
		for _, sidecar := range []string{path, manifestPath, path + ".artifacts", path + ".complete.json"} {
			if _, statErr := os.Stat(sidecar); statErr == nil {
				if removeErr := os.RemoveAll(sidecar); removeErr != nil {
					return nil, removeErr
				}
				removed = append(removed, sidecar)
			} else if !os.IsNotExist(statErr) {
				return nil, statErr
			}
		}
	}
	return removed, nil
}

func isBackupTempName(name string) bool {
	if !strings.HasPrefix(name, ".") {
		return false
	}
	for _, marker := range []string{".db-", ".artifacts-", ".manifest-", ".complete-", ".missis-"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func readToolBackupManifest(path string) (missis.BackupManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return missis.BackupManifest{}, err
	}
	defer file.Close()
	var manifest missis.BackupManifest
	if err := json.NewDecoder(file).Decode(&manifest); err != nil {
		return missis.BackupManifest{}, err
	}
	return manifest, nil
}
