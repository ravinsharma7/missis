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

func RunRemote(args []string, stdout, stderr io.Writer) int {
	return runRemote(args, stdout, stderr, "store-remote", false)
}

func RunRemoteWithName(args []string, stdout, stderr io.Writer, commandName string) int {
	return runRemote(args, stdout, stderr, commandName, true)
}

func runRemote(args []string, stdout, stderr io.Writer, commandName string, strictArgs bool) int {
	stdout, stderr = commandWriters(stdout, stderr)
	if len(args) < 1 {
		fmt.Fprintf(stderr, "usage: %s <upload|download> [args]\n", commandName)
		return 2
	}
	if strictArgs {
		switch args[0] {
		case "upload":
			if len(args) > 2 {
				fmt.Fprintf(stderr, "usage: %s upload [source]\n", commandName)
				return 2
			}
		case "download":
			if len(args) != 2 {
				fmt.Fprintf(stderr, "usage: %s download <destination>\n", commandName)
				return 2
			}
		default:
			fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
			return 2
		}
	}
	loadEnvFile(".env.local")
	ctx := context.Background()

	svc, err := application.Open("")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	client := missis.NewClient(svc)
	defer client.Close()
	manifest, err := client.Manifest(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	remote, err := resolveRemote()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	switch args[0] {
	case "upload":
		src := ""
		if len(args) > 1 {
			src = args[1]
		}
		if src == "" {
			src = defaultBackupPath(manifest)
		}
		if _, err := os.Stat(src); err != nil {
			fmt.Fprintf(stderr, "backup not found: %s\n", src)
			return 1
		}
		key, err := uploadBackup(ctx, remote, manifest, src, os.Getenv("MISSIS_BACKUP_FORCE") == "1")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "uploaded %s\n", key)
	case "download":
		if !strictArgs && len(args) < 2 {
			fmt.Fprintf(stderr, "usage: %s download <destination>\n", commandName)
			return 2
		}
		if err := downloadAndVerify(ctx, remote, manifest, args[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "downloaded backup verified")
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
	return 0
}

func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

func defaultBackupPath(manifest missis.ManifestInfo) string {
	name := strings.ReplaceAll(manifest.StoreID, ":", "_") + "-" + manifest.HeadHash + ".db"
	return filepath.Join("backups", name)
}
