package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ravinsharma7/missis/implementation/store"
)

func main() {
	path := resolvePath()
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer s.Close()

	storeID, err := s.StoreID()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	headHash, err := s.HeadHash()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	eventCount, err := s.EventCount()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	schemaVersion, err := s.SchemaVersion()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	manifest := map[string]any{
		"schema_version": schemaVersion,
		"store_id":       storeID,
		"head_hash":      headHash,
		"event_count":    eventCount,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolvePath() string {
	if len(os.Args) > 1 {
		return os.Args[1]
	}
	if env := os.Getenv("MISSIS_STORE"); env != "" {
		return env
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "missis", "missis.db")
}
