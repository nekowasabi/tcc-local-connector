//go:build !windows

package ledger

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

var psPath = "/bin/ps"

func processIdentity(ctx context.Context, pid int) (string, string, bool, error) {
	output, err := exec.CommandContext(ctx, psPath, "-p", strconv.Itoa(pid), "-o", "lstart=,args=").Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	line := strings.TrimSuffix(string(output), "\n")
	if line == "" {
		return "", "", false, nil
	}
	index := strings.Index(line, " ")
	for index >= 0 && index < len(line)-1 && strings.TrimSpace(line[:index]) == "" {
		index++
	}
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return "", "", false, nil
	}
	start := strings.Join(fields[:5], " ")
	args := strings.TrimSpace(strings.TrimPrefix(line, start))
	return start, args, true, nil
}
