//go:build !windows

package store

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLockFile(file *os.File, exclusive bool) (func() error, bool, error) {
	flags := unix.LOCK_NB
	if exclusive {
		flags |= unix.LOCK_EX
	} else {
		flags |= unix.LOCK_SH
	}
	if err := unix.Flock(int(file.Fd()), flags); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return func() error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }, false, nil
}
