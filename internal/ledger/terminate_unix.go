//go:build !windows

package ledger

import "syscall"

func terminateProcess(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
