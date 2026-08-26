//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/takets/tcc-local-connector/internal/constants"
)

func openVerified(path string) (*os.File, error) {
	// Why: O_NOFOLLOW plus Fstat keeps the checked object identical to the one decoded.
	fd, err := syscall.Open(filepath.Clean(path), syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot open configuration", ErrInsecurePermissions)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&constants.ConfigForbiddenModeMask != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: owner or mode is not safe", ErrInsecurePermissions)
	}
	return file, nil
}
