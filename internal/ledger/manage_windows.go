//go:build windows

package ledger

import (
	"os/exec"
	"strconv"
)

func terminateManaged(pid int) error {
	// Why: taskkill without /F sends WM_CLOSE. /F would be force terminate, which this project refuses.
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid))
	return cmd.Run()
}
