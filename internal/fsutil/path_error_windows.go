//go:build windows

package fsutil

import (
	"path/filepath"
	"strings"
)

func platformPathTooLong(path, message string) bool {
	if !strings.Contains(message, "filename, directory name, or volume label syntax is incorrect") {
		return false
	}
	cleaned := filepath.Clean(path)
	if len(cleaned) >= 260 {
		return true
	}
	for _, component := range strings.FieldsFunc(cleaned, func(r rune) bool { return r == '\\' || r == '/' }) {
		if len(component) > 255 {
			return true
		}
	}
	return false
}
