package main

import "testing"

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
