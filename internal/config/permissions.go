package config

import (
	"errors"
	"fmt"
	"os"
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
	return openVerified(path)
}
