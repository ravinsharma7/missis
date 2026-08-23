//go:build !windows

package fsutil

func platformPathTooLong(_, _ string) bool { return false }
