//go:build !windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/takets/tcc-local-connector/internal/constants"
)

var ErrInsecurePermissions = errors.New("insecure_permissions")

// CheckPermissions opens and validates one regular configuration file. The
// descriptor is returned so validation and decoding use the same inode.
func CheckPermissions(path string) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: configuration path is required", ErrInsecurePermissions)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: configuration must be a regular file", ErrInsecurePermissions)
	}
	// Why: O_NOFOLLOW plus Fstat keeps the checked object identical to the one decoded.
	fd, err := syscall.Open(filepath.Clean(path), syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot open configuration", ErrInsecurePermissions)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err = file.Stat()
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

// defaultTaskSourceExecutable has no platform-specific default outside Windows.
func defaultTaskSourceExecutable() string {
	return ""
}
