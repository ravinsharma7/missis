//go:build !linux

package peerconfig

import "os"

func validateConfigOwner(os.FileInfo) error { return nil }
