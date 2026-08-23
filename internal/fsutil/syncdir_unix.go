//go:build !windows

package fsutil

import "os"

// SyncDir makes directory-entry changes durable on platforms that support
// fsync on an open directory descriptor.
func SyncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
