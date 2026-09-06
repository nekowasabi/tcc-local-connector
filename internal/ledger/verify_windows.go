//go:build windows

package ledger

import (
	"context"
	"encoding/csv"
	"os/exec"
	"strconv"
	"strings"
)

func VerifyEntry(ctx context.Context, entry Entry) (bool, error) {
	start, args, live, err := processIdentity(ctx, entry.PID)
	if err != nil || !live {
		return false, err
	}
	return start == entry.PSLstart && args == entry.PSArgs, nil
}

// processIdentity reports liveness via tasklist.
// Why: os.FindProcess always succeeds on Windows, and tasklist is the only
// stdlib-free way to query a PID without wmic or PowerShell. Start time is not
// available, so identity is the image name only.
func processIdentity(ctx context.Context, pid int) (string, string, bool, error) {
	filter := "PID eq " + strconv.Itoa(pid)
	output, err := exec.CommandContext(ctx, "tasklist", "/FI", filter, "/FO", "CSV", "/NH").Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	// Why: a no-match prints an INFO line, not CSV, so only well-formed rows count.
	reader := csv.NewReader(strings.NewReader(string(output)))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		// Why: returning false, nil would make the caller drop the entry as orphan_dropped (fail-open).
		return "", "", false, err
	}
	for _, record := range records {
		if len(record) >= 2 && record[1] == strconv.Itoa(pid) {
			return "", record[0], true, nil
		}
	}
	return "", "", false, nil
}
