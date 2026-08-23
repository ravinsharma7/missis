//go:build windows

package fsutil

import (
	"errors"
	"strings"
	"testing"
)

func TestIsPathTooLongRecognizesWindowsInvalidNameForLongPath(t *testing.T) {
	err := errors.New("The filename, directory name, or volume label syntax is incorrect.")
	if !IsPathTooLong(`C:\temp\`+strings.Repeat("x", 300), err) {
		t.Fatal("long Windows component was not recognized")
	}
	if IsPathTooLong(`C:\temp\short`, err) {
		t.Fatal("short invalid Windows path was classified as too long")
	}
}
