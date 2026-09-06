//go:build windows

package ledger

import "os"

// Why: Windows has no graceful SIGTERM equivalent, so the process is killed outright.
func terminateProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
