//go:build windows

package engine

import (
	"context"
	"os/exec"
	"syscall"
)

// Why: cmd.exe is the only shell guaranteed on Windows; /C mirrors sh -c.
// Why: bypass Go's EscapeArg, which turns inner quotes into \" that cmd.exe does not understand.
func shellCommand(ctx context.Context, commandLine string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /S /C \"" + commandLine + "\""}
	return cmd
}
