//go:build linux

package peerconfig

import (
	"fmt"
	"os"
	"syscall"
)

func validateConfigOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("peer-config-invalid: cannot inspect config ownership")
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("peer-config-invalid: config must be owned by the current user")
	}
	return nil
}
