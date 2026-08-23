//go:build !windows

package update

import "fmt"

func waitForProcessExit(pid int) error {
	return fmt.Errorf("waiting for process %d is only supported by the Windows update helper", pid)
}
