package tooling

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func RunBackupWithName(args []string, stdout, stderr io.Writer, commandName string) int {
	if len(args) > 0 {
		switch args[0] {
		case "verify":
			return runBackupVerify(args[1:], stdout, stderr, commandName+" verify")
		case "cleanup":
			return runBackupCleanup(args[1:], stdout, stderr, commandName+" cleanup")
		}
	}
	return runBackup(args, stdout, stderr, commandName)
}

func runBackup(args []string, stdout, stderr io.Writer, commandName string) int {
	stdout, stderr = commandWriters(stdout, stderr)
	if len(args) > 1 {
		fmt.Fprintf(stderr, "usage: %s [destination]\n", commandName)
		return 2
	}
	resolved, err := missis.ResolveStore("")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	svc, err := application.Open("")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	manifest, err := client.Manifest(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	destination := ""
	derived := len(args) == 0
	if derived {
		destination, err = defaultBackupPath(resolved, manifest)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		destination = filepath.Clean(args[0])
	}
	if derived {
		if _, statErr := os.Stat(destination); statErr == nil {
			if err := client.VerifyRestore(ctx, destination, manifest); err != nil {
				fmt.Fprintf(stderr, "existing backup does not match current store: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "backup already exists: %s (verified current store)\n", destination)
			return 0
		} else if !os.IsNotExist(statErr) {
			fmt.Fprintln(stderr, statErr)
			return 1
		}
	}
	if err := client.BackupTo(ctx, destination); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if derived {
		fmt.Fprintln(stdout, destination)
	}
	return 0
}

func defaultBackupPath(resolved missis.ResolvedStore, manifest missis.ManifestInfo) (string, error) {
	projectDir := filepath.Dir(resolved.Path)
	if resolved.MarkerDir != "" {
		projectDir = resolved.MarkerDir
	} else if filepath.Base(projectDir) == ".missis-store" {
		projectDir = filepath.Dir(projectDir)
	}
	backupDir := os.Getenv("MISSIS_BACKUP_DIR")
	if backupDir == "" {
		backupDir = filepath.Join(projectDir, ".missis-backups")
	} else if !filepath.IsAbs(backupDir) {
		backupDir = filepath.Join(projectDir, backupDir)
	}
	absDir, err := filepath.Abs(backupDir)
	if err != nil {
		return "", fmt.Errorf("resolve backup directory: %w", err)
	}
	name := strings.ReplaceAll(manifest.StoreID, ":", "_") + "-" + manifest.HeadHash + ".db"
	return filepath.Join(filepath.Clean(absDir), name), nil
}
