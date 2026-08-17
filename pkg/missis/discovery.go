package missis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverySource identifies where a store path came from. Explicit process
// input (--store, MISSIS_STORE) outranks repository-controlled input
// (.missis marker), which outranks the XDG default.
type DiscoverySource string

const (
	DiscoveryFlag    DiscoverySource = "flag"
	DiscoveryEnv     DiscoverySource = "env"
	DiscoveryMarker  DiscoverySource = "marker"
	DiscoveryDefault DiscoverySource = "default"
)

// ResolvedStore is the outcome of store discovery: the absolute path to open,
// the path as supplied by the source, and the source itself.
type ResolvedStore struct {
	Path      string
	Supplied  string
	Source    DiscoverySource
	MarkerDir string // repo root containing the .missis marker, when used
}

// ResolveStore resolves the store path with an explicit trust policy:
//
//  1. --store wins over everything.
//  2. MISSIS_STORE wins over repository markers.
//  3. A .missis marker is honored only when its path is relative and stays
//     inside the marker's project root (symlinks resolved); absolute or
//     escaping marker paths are rejected.
//  4. The user XDG default is the fallback.
func ResolveStore(storeFlag string) (ResolvedStore, error) {
	if storeFlag != "" {
		abs, err := filepath.Abs(storeFlag)
		if err != nil {
			return ResolvedStore{}, err
		}
		return ResolvedStore{Path: filepath.Clean(abs), Supplied: storeFlag, Source: DiscoveryFlag}, nil
	}
	if env := os.Getenv("MISSIS_STORE"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return ResolvedStore{}, err
		}
		return ResolvedStore{Path: filepath.Clean(abs), Supplied: env, Source: DiscoveryEnv}, nil
	}
	if raw, dir, ok, err := findMissisMarker(); err != nil {
		return ResolvedStore{}, err
	} else if ok {
		if filepath.IsAbs(raw) {
			return ResolvedStore{}, fmt.Errorf(".missis marker contains an absolute path (%s); use MISSIS_STORE or --store for external stores", raw)
		}
		root := filepath.Clean(dir)
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			root = resolved
		}
		target := filepath.Join(root, filepath.Clean(raw))
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ResolvedStore{}, fmt.Errorf(".missis marker escapes the project root (%s); use MISSIS_STORE or --store for external stores", raw)
		}
		return ResolvedStore{Path: filepath.Clean(target), Supplied: raw, Source: DiscoveryMarker, MarkerDir: root}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ResolvedStore{Path: filepath.Clean(filepath.Join(".", ".missis", "missis.db")), Supplied: ".missis/missis.db", Source: DiscoveryDefault}, nil
	}
	defaultPath := filepath.Join(home, ".local", "share", "missis", "missis.db")
	return ResolvedStore{Path: filepath.Clean(defaultPath), Supplied: defaultPath, Source: DiscoveryDefault}, nil
}

// ResolveStorePath is the path-only form of ResolveStore, kept for callers
// that do not need the discovery source.
func ResolveStorePath(storeFlag string) (string, error) {
	resolved, err := ResolveStore(storeFlag)
	if err != nil {
		return "", err
	}
	return resolved.Path, nil
}

// findMissisMarker walks up from the working directory looking for .missis.
// It returns the raw marker content (relative to dir, or absolute), the
// directory containing the marker, and whether a marker was found.
func findMissisMarker() (raw string, dir string, ok bool, err error) {
	dir, err = os.Getwd()
	if err != nil {
		return "", "", false, nil
	}
	for {
		marker := filepath.Join(dir, ".missis")
		info, statErr := os.Stat(marker)
		if statErr == nil {
			if info.IsDir() {
				return ".missis/missis.db", dir, true, nil
			}
			data, readErr := os.ReadFile(marker)
			if readErr != nil {
				return "", "", false, readErr
			}
			line := strings.TrimSpace(string(data))
			if line == "" {
				return "", "", false, fmt.Errorf(".missis marker is empty; expected one store path line")
			}
			if strings.ContainsRune(line, '\n') {
				return "", "", false, fmt.Errorf(".missis marker must contain exactly one line")
			}
			if strings.ContainsRune(line, 0) {
				return "", "", false, fmt.Errorf(".missis marker contains a NUL byte")
			}
			return line, dir, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false, nil
		}
		dir = parent
	}
}
