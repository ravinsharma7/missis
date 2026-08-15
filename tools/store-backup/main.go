package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ravinsharma7/missis/implementation/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: store-backup <destination>")
		os.Exit(1)
	}
	src := resolveSource()
	dst := os.Args[1]
	s, err := store.Open(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer s.Close()
	if err := s.Backup(dst); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolveSource() string {
	if env := os.Getenv("MISSIS_STORE"); env != "" {
		return env
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "missis", "missis.db")
}
