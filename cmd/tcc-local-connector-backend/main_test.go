package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestConfig_ValidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte("version: 2\ntask_source:\n  executable: /usr/bin/true\nrules: []\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testConfig(path, io.Discard); err != nil {
		t.Fatalf("testConfig() error = %v", err)
	}
}

func TestParseOptions(t *testing.T) {
	options, err := parseOptions([]string{
		"serve",
		"--stdio",
		"--tcc2-executable",
		"/home/linuxbrew/.linuxbrew/bin/tcc2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.tcc2Executable != "/home/linuxbrew/.linuxbrew/bin/tcc2" {
		t.Fatalf("unexpected executable: %q", options.tcc2Executable)
	}
}

func TestParseOptionsAcceptsConfigTest(t *testing.T) {
	options, err := parseOptions([]string{"config-test", "--config", "/tmp/config.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if options.command != "config-test" || options.configPath != "/tmp/config.yml" {
		t.Fatalf("options = %#v", options)
	}
}

func TestParseOptionsAcceptsRunOnce(t *testing.T) {
	options, err := parseOptions([]string{"run-once", "--config", "/tmp/config.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if options.command != "run-once" {
		t.Fatalf("command = %q", options.command)
	}
	if !options.dryRun {
		t.Fatal("run-once must default to dry-run")
	}
}

func TestParseOptionsRunOnceApply(t *testing.T) {
	options, err := parseOptions([]string{"run-once", "--apply"})
	if err != nil {
		t.Fatal(err)
	}
	if options.dryRun {
		t.Fatal("--apply must disable dry-run")
	}
}

func TestParseOptionsRejectsConflictingRunOnceModes(t *testing.T) {
	if _, err := parseOptions([]string{"run-once", "--apply", "--dry-run"}); err == nil {
		t.Fatal("conflicting run-once modes were accepted")
	}
}

func TestRunOnceEmitsJSON(t *testing.T) {
	path := writeRunConfig(t)
	var output bytes.Buffer
	if err := runOnce(path, &output, true); err != nil {
		t.Fatal(err)
	}
	var result struct {
		DryRun  bool `json:"dry_run"`
		Actions []struct {
			Phase string `json:"phase"`
			Title string `json:"title"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.DryRun {
		t.Fatal("run-once output did not report dry_run=true")
	}
	if len(result.Actions) != 3 {
		t.Fatalf("run-once actions = %#v", result.Actions)
	}
	if result.Actions[0].Phase != "default" || result.Actions[1].Phase != "default" || result.Actions[2].Phase != "rules" {
		t.Fatalf("run-once phases = %#v", result.Actions)
	}
}

func TestRunBackendCommands(t *testing.T) {
	path := writeRunConfig(t)
	for _, command := range []string{
		"validate", "config-test", "config-paths", "status", "pause", "resume", "refresh-now", "run-once",
	} {
		args := []string{command, "--config", path}
		if command == "pause" {
			args = append(args, "--duration-seconds", "60")
		}
		var output bytes.Buffer
		if err := run(args, &output); err != nil {
			t.Fatalf("run(%q) error = %v", command, err)
		}
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	if err := run(nil, io.Discard); err == nil {
		t.Fatal("run(nil) unexpectedly succeeded")
	}
	if err := run([]string{"unknown"}, io.Discard); err == nil {
		t.Fatal("run(unknown) unexpectedly succeeded")
	}
	for _, args := range [][]string{
		{},
		{"unknown"},
		{"serve", "--stdio", "--tcc2-executable", ""},
		{"pause"},
		{"pause", "--duration-seconds", "86401"},
		{"status", "--dry-run"},
		{"run-once", "--config", filepath.Join(t.TempDir(), "missing.yml")},
	} {
		if _, err := parseOptions(args); err == nil && len(args) > 0 && args[0] != "run-once" {
			t.Fatalf("parseOptions(%v) unexpectedly succeeded", args)
		}
	}
}

func TestRunReportsInvalidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yml")
	if err := os.WriteFile(path, []byte("version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"validate", "config-test", "run-once", "status"} {
		if err := run([]string{command, "--config", path}, io.Discard); err == nil {
			t.Fatalf("run(%q) accepted invalid configuration", command)
		}
	}
}

func writeRunConfig(t *testing.T) string {
	t.Helper()
	fake, err := filepath.Abs("../../internal/engine/testdata/fake-tcc2.sh")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	content := []byte("version: 2\ntask_source:\n  executable: " + fake + "\npolling:\n  timeout_seconds: 1\ndefault:\n  on_task_start:\n    - type: notify\n      title: Default one\n      message: First\n    - type: notify\n      title: Default two\n      message: Second\nrules:\n  - id: active\n    match:\n      task_name_contains: [E2E]\n    ensure:\n      - type: notify\n        title: Rule\n        message: Third\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseOptionsRejectsMissingStdio(t *testing.T) {
	if _, err := parseOptions([]string{"serve"}); err == nil {
		t.Fatal("missing --stdio was accepted")
	}
}

func TestParseOptionsRejectsUnexpectedArgument(t *testing.T) {
	if _, err := parseOptions([]string{"serve", "--stdio", "extra"}); err == nil {
		t.Fatal("unexpected positional argument was accepted")
	}
}

func TestParseOptionsAcceptsConfigPath(t *testing.T) {
	options, err := parseOptions([]string{
		"serve",
		"--stdio",
		"--config",
		"/tmp/custom-config.yml",
		"--tcc2-executable",
		"/home/linuxbrew/.linuxbrew/bin/tcc2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.configPath != "/tmp/custom-config.yml" {
		t.Fatalf("unexpected config path: %q", options.configPath)
	}
}

func TestParseOptionsAcceptsBackendCommands(t *testing.T) {
	for _, command := range []string{"validate", "config-test", "run-once", "resume", "refresh-now", "config-paths", "status"} {
		if got, err := parseOptions([]string{command}); err != nil || got.command != command {
			t.Fatalf("parseOptions(%q) = %#v, %v", command, got, err)
		}
	}
	if got, err := parseOptions([]string{"pause", "--duration-seconds", "60"}); err != nil || got.command != "pause" {
		t.Fatalf("pause options = %#v, %v", got, err)
	}
}

func TestTestConfigRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testConfig(path, io.Discard); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("testConfig() error = %v", err)
	}
}
