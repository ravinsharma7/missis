//go:build windows

package fsutil

import "os"

// SyncDir validates that the published directory can be opened. Go's Windows
// implementation does not support Sync on directory handles; durable files
// are flushed before rename and the directory handle is closed here.
func SyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return dir.Close()
}
