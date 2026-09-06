//go:build !windows

package engine

import (
	"context"
	"os/exec"
)

func shellCommand(ctx context.Context, commandLine string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", commandLine)
}
