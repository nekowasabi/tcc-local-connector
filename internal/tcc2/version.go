package tcc2

import (
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var cellarVersionRE = regexp.MustCompile(`(?:^|/)Cellar/tcc2/([^/]+)/`)

func ResolveCLIVersion(executable string) string {
	path, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return ""
	}
	match := cellarVersionRE.FindStringSubmatch(path)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}
func ResolveAuthStatus(ctx context.Context, executable string) bool {
	output, err := exec.CommandContext(ctx, executable, "status").Output()
	return err == nil && strings.Contains(string(output), "Logged in as: ")
}
