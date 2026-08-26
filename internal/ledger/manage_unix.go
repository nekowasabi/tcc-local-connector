//go:build !windows

package ledger

import "syscall"

func terminateManaged(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
