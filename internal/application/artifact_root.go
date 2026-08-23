package application

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/fsutil"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type artifactRootResolution struct {
	root    string
	warning string
}

// NamespacedArtifactRoot returns the current default root without applying the
// legacy-root compatibility policy. Maintenance commands use this when they
// need to migrate or inspect the new layout directly.
func NamespacedArtifactRoot(storeID string) (string, error) {
	return namespacedArtifactRoot(storeID)
}

// LegacyArtifactRoot returns the only layout considered legacy: an artifacts
// directory beside the SQLite store.
func LegacyArtifactRoot(storePath string) string {
	return filepath.Join(filepath.Dir(storePath), "artifacts")
}

// ArtifactRootForMaintenance resolves an explicit override first and otherwise
// returns the new per-store namespace. It intentionally does not select a
// legacy root; migration and garbage collection must make that choice explicit.
func ArtifactRootForMaintenance(storeID, override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return filepath.Clean(override), nil
	}
	return namespacedArtifactRoot(storeID)
}

func resolveArtifactRoot(resolved missis.ResolvedStore, storeID, override string) (artifactRootResolution, error) {
	if strings.TrimSpace(override) != "" {
		return artifactRootResolution{root: filepath.Clean(override)}, nil
	}
	if strings.TrimSpace(storeID) == "" {
		return artifactRootResolution{}, fmt.Errorf("cannot resolve artifact store namespace: store identity is empty; set MISSIS_ARTIFACT_STORE")
	}
	dataDir, err := platformUserDataDir()
	if err != nil || strings.TrimSpace(dataDir) == "" {
		if err == nil {
			err = fmt.Errorf("user data directory is empty")
		}
		return artifactRootResolution{}, fmt.Errorf("cannot resolve default artifact store root: %w; set MISSIS_ARTIFACT_STORE", err)
	}
	digest := sha256.Sum256([]byte(storeID))
	newRoot := filepath.Join(dataDir, "missis", "artifacts", hex.EncodeToString(digest[:16]))
	legacyRoot := filepath.Join(filepath.Dir(resolved.Path), "artifacts")
	legacyValid, err := validLegacyArtifactRoot(legacyRoot)
	if err != nil {
		return artifactRootResolution{}, err
	}
	newExists, err := pathExists(newRoot)
	if err != nil {
		return artifactRootResolution{}, err
	}
	if legacyValid && newExists {
		return artifactRootResolution{}, fmt.Errorf("legacy artifact root %q and namespaced root %q both exist; set MISSIS_ARTIFACT_STORE explicitly or migrate one root", legacyRoot, newRoot)
	}
	if legacyValid {
		return artifactRootResolution{
			root:    legacyRoot,
			warning: fmt.Sprintf("using legacy artifact root %q; migrate it before removing the legacy project-local location", legacyRoot),
		}, nil
	}
	return artifactRootResolution{root: newRoot}, nil
}

// platformUserDataDir provides the user-data location used by the current Go
// release's platform conventions. Go exposes UserConfigDir, but not a
// UserDataDir helper, so data-specific locations are selected explicitly.
func platformUserDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if value := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); value != "" {
			return value, nil
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if value := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); value != "" {
			return value, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	}
	return os.UserHomeDir()
}

func validLegacyArtifactRoot(root string) (bool, error) {
	info, err := os.Stat(filepath.Join(root, "sha256"))
	if err == nil {
		return info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	if artifactErr := pathLengthError(filepath.Join(root, "sha256"), err); artifactErr != nil {
		return false, artifactErr
	}
	return false, err
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	if wrapped := pathLengthError(path, err); wrapped != nil {
		return false, wrapped
	}
	return false, err
}

func pathLengthError(path string, err error) error {
	if err == nil {
		return nil
	}
	if fsutil.IsPathTooLong(path, err) {
		return fmt.Errorf("%w: %s: %v", artifact.ErrPathTooLong, path, err)
	}
	return nil
}
