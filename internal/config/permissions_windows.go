//go:build windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrInsecurePermissions = errors.New("insecure_permissions")

// CheckPermissions opens and validates one regular configuration file.
// Why: Windows has no POSIX owner/mode bits, so the ACL check is skipped;
// only the regular-file and non-symlink checks are kept.
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
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot open configuration", ErrInsecurePermissions)
	}
	return file, nil
}

// defaultTaskSourceExecutable returns the Scoop shim path for tcc2 if present.
// Why: scoop install tcc2 で入れた tcc2 を設定なしで使うため。
func defaultTaskSourceExecutable() string {
	root := os.Getenv("SCOOP")
	if root == "" {
		root = filepath.Join(os.Getenv("USERPROFILE"), "scoop")
	}
	shim := filepath.Join(root, "shims", "tcc2.exe")
	info, err := os.Stat(shim)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return shim
}
