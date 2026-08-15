package missis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveStorePath(storeFlag string) (string, error) {
	if storeFlag != "" {
		return storeFlag, nil
	}
	marker, err := findMissisMarker()
	if err != nil {
		return "", err
	}
	if marker != "" {
		return marker, nil
	}
	if env := os.Getenv("MISSIS_STORE"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".missis", "missis.db"), nil
	}
	return filepath.Join(home, ".local", "share", "missis", "missis.db"), nil
}

func findMissisMarker() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", nil
	}
	for {
		marker := filepath.Join(dir, ".missis")
		info, statErr := os.Stat(marker)
		if statErr == nil {
			if info.IsDir() {
				return filepath.Join(marker, "missis.db"), nil
			}
			data, readErr := os.ReadFile(marker)
			if readErr != nil {
				return "", readErr
			}
			line := strings.TrimSpace(string(data))
			if line == "" {
				return "", fmt.Errorf(".missis marker is empty; expected one store path line")
			}
			if strings.ContainsRune(line, '\n') {
				return "", fmt.Errorf(".missis marker must contain exactly one line")
			}
			if strings.ContainsRune(line, 0) {
				return "", fmt.Errorf(".missis marker contains a NUL byte")
			}
			if filepath.IsAbs(line) {
				return line, nil
			}
			return filepath.Join(dir, line), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}
