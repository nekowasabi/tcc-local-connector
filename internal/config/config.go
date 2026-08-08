package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/takets/tcc-local-connector/internal/constants"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Version    int        `yaml:"version" json:"version"`
	Polling    Polling    `yaml:"polling" json:"polling"`
	TaskSource TaskSource `yaml:"task_source" json:"task_source"`
	Safety     Safety     `yaml:"safety" json:"safety"`
	Logging    Logging    `yaml:"logging" json:"logging"`
	Rules      []Rule     `yaml:"rules" json:"rules"`
}
type Polling struct {
	IntervalSeconds     int    `yaml:"interval_seconds" json:"interval_seconds"`
	TimeoutSeconds      int    `yaml:"timeout_seconds" json:"timeout_seconds"`
	FailureGraceSeconds int    `yaml:"failure_grace_seconds" json:"failure_grace_seconds"`
	FailurePolicy       string `yaml:"failure_policy" json:"failure_policy"`
}
type TaskSource struct {
	Type       string   `yaml:"type" json:"type"`
	Executable string   `yaml:"executable" json:"executable"`
	Args       []string `yaml:"args" json:"args"`
	ViewID     *string  `yaml:"view_id" json:"view_id"`
}
type Safety struct {
	DryRun                      bool `yaml:"dry_run" json:"dry_run"`
	AllowShell                  bool `yaml:"allow_shell" json:"allow_shell"`
	AllowExternalProcessControl bool `yaml:"allow_external_process_control" json:"allow_external_process_control"`
	AllowForceTerminate         bool `yaml:"allow_force_terminate" json:"allow_force_terminate"`
}
type Logging struct {
	Level      string `yaml:"level" json:"level"`
	RetainDays int    `yaml:"retain_days" json:"retain_days"`
}
type Rule struct {
	ID       string   `yaml:"id" json:"id"`
	Priority int      `yaml:"priority" json:"priority"`
	Match    Match    `yaml:"match" json:"match"`
	Ensure   []Action `yaml:"ensure" json:"ensure"`
	OnEnter  []Action `yaml:"on_enter" json:"on_enter"`
	OnExit   []Action `yaml:"on_exit" json:"on_exit"`
}
type Match struct {
	TaskNameContains    []string `yaml:"task_name_contains" json:"task_name_contains"`
	TaskNameNotContains []string `yaml:"task_name_not_contains" json:"task_name_not_contains"`
}
type Action struct {
	Type           string            `yaml:"type" json:"type"`
	BundleID       string            `yaml:"bundle_id" json:"bundle_id"`
	ProcessID      string            `yaml:"process_id" json:"process_id"`
	Executable     string            `yaml:"executable" json:"executable"`
	Args           []string          `yaml:"args" json:"args"`
	WorkingDir     string            `yaml:"working_dir" json:"working_dir"`
	Env            map[string]string `yaml:"env" json:"env"`
	GraceSeconds   int               `yaml:"grace_seconds" json:"grace_seconds"`
	TimeoutSeconds int               `yaml:"timeout_seconds" json:"timeout_seconds"`
	Shell          bool              `yaml:"shell" json:"shell"`
	Title          string            `yaml:"title" json:"title"`
	Message        string            `yaml:"message" json:"message"`
	Level          string            `yaml:"level" json:"level"`
}

func defaults() Config {
	return Config{Version: constants.ConfigSchemaVersion, Polling: Polling{IntervalSeconds: constants.DefaultPollIntervalSeconds, TimeoutSeconds: constants.DefaultPollTimeoutSeconds, FailureGraceSeconds: constants.DefaultFailureGraceSeconds, FailurePolicy: "release_controls"}, TaskSource: TaskSource{Type: "tcc2_mcp", Args: []string{"mcp"}}, Logging: Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays}, Rules: []Rule{}}
}

func Load(path string) (*Config, []ValidationError, error) {
	f, err := CheckPermissions(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	payload, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	cfg := defaults()
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if isUnknownFieldError(err) {
			return nil, []ValidationError{{Code: codeUnknownField, Message: "configuration contains unknown field"}}, nil
		}
		return nil, []ValidationError{{Code: codeParseError, Message: "configuration syntax is invalid"}}, nil
	}
	if cfg.Safety.AllowShell {
		info, statErr := f.Stat()
		if statErr != nil || info.Mode().Perm()&constants.ConfigStrictModeMask != 0 {
			return nil, nil, fmt.Errorf("%w: shell-enabled configuration requires private mode", ErrInsecurePermissions)
		}
	}
	validated, validationErrors := Validate(&cfg)
	if len(validationErrors) > 0 {
		return nil, validationErrors, nil
	}
	return validated, nil, nil
}

func isUnknownFieldError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "field") && strings.Contains(msg, "not found")
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, constants.ConfigRelPath), nil
}
