//go:build windows

package config

import (
	"fmt"
	"os"
)

func openVerified(path string) (*os.File, error) {
	// Why: Windows has no O_NOFOLLOW/uid/mode equivalent of the Unix check.
	// Rejecting non-regular files happens in CheckPermissions; ACL hardening is later.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot open configuration", ErrInsecurePermissions)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("%w: configuration must be a regular file", ErrInsecurePermissions)
	}
	return file, nil
}
