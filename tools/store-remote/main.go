package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: store-remote <upload|download> [args]")
		os.Exit(2)
	}
	loadEnvFile(".env.local")
	ctx := context.Background()

	svc, err := application.Open("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	manifest, err := client.Manifest(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	remote, err := resolveRemote()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "upload":
		src := ""
		if len(os.Args) > 2 {
			src = os.Args[2]
		}
		if src == "" {
			src = defaultBackupPath(manifest)
		}
		if _, err := os.Stat(src); err != nil {
			fmt.Fprintf(os.Stderr, "backup not found: %s\n", src)
			os.Exit(1)
		}
		key, err := uploadBackup(ctx, remote, manifest, src, os.Getenv("MISSIS_BACKUP_FORCE") == "1")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("uploaded %s\n", key)
	case "download":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: store-remote download <destination>")
			os.Exit(2)
		}
		if err := downloadAndVerify(ctx, remote, manifest, os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("downloaded backup verified")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
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
