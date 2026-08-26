package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func ResolveBackendPath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("backend not found: %w", err)
		}
		return explicit, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	name := "tcc-local-connector-backend"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidate := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(candidate); err != nil {
		return "", fmt.Errorf("backend not found next to %s", exe)
	}
	return candidate, nil
}
