package fsutil

import "strings"

// IsPathTooLong recognizes platform path-length failures without treating an
// arbitrary invalid path as a length failure.
func IsPathTooLong(path string, err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "file name too long") ||
		strings.Contains(message, "filename too long") ||
		strings.Contains(message, "path too long") ||
		strings.Contains(message, "name too long") {
		return true
	}
	return platformPathTooLong(path, message)
}
