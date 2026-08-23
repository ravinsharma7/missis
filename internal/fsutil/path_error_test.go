package fsutil

import (
	"errors"
	"testing"
)

func TestIsPathTooLongRecognizesPortableMessages(t *testing.T) {
	for _, message := range []string{"file name too long", "filename too long", "path too long", "name too long"} {
		if !IsPathTooLong("any", errors.New(message)) {
			t.Fatalf("message %q was not recognized", message)
		}
	}
	if IsPathTooLong("short", errors.New("permission denied")) {
		t.Fatal("unrelated path error classified as too long")
	}
}
