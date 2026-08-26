//go:build windows

package ledger

import (
	"context"
	"time"

	"golang.org/x/sys/windows"
)

func processIdentity(ctx context.Context, pid int) (string, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", "", false, nil
	}
	defer windows.CloseHandle(handle)

	var buf [windows.MAX_PATH]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "", "", false, nil
	}
	path := windows.UTF16ToString(buf[:size])

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return "", "", false, nil
	}
	start := time.Unix(0, creation.Nanoseconds()).UTC().Format(time.RFC3339Nano)
	return start, path, true, nil
}
