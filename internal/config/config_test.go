package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/takets/tcc-local-connector/internal/constants"
)

func TestValidateAcceptsMinimalConfig(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	if got, errs := Validate(&cfg); got == nil || len(errs) != 0 {
		t.Fatalf("Validate() = %#v, %#v", got, errs)
	}
}

func TestValidateRejectsUnsafeAndInvalidValues(t *testing.T) {
	cfg := defaults()
	cfg.Version = 99
	cfg.TaskSource.Executable = "/missing"
	cfg.TaskSource.Type = "other"
	cfg.Polling.TimeoutSeconds = cfg.Polling.IntervalSeconds
	cfg.Safety.AllowForceTerminate = true
	cfg.Logging.Level = "trace"
	cfg.Rules = []Rule{{ID: "bad id", Ensure: []Action{{Type: "unknown"}}}}
	_, errs := Validate(&cfg)
	if len(errs) < 6 {
		t.Fatalf("got %d validation errors, want several", len(errs))
	}
}

func TestLoadRejectsMalformedAndUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("version: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errs, err := Load(path); err != nil || len(errs) != 1 || errs[0].Code != codeParseError {
		t.Fatalf("malformed Load() = %#v, %#v", errs, err)
	}
	if constants.ConfigSchemaVersion == 0 {
		t.Fatal("invalid schema constant")
	}
}

func TestCheckPermissionsRejectsEmptyAndSymlink(t *testing.T) {
	if _, err := CheckPermissions(""); err == nil {
		t.Fatal("empty path accepted")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckPermissions(link); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestConfigValidationCoversActionKindsAndBoundaries(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	cfg.Rules = []Rule{{ID: "ok", Priority: 1000, Match: Match{TaskNameContains: []string{"x"}}, Ensure: []Action{
		{Type: "app.start", BundleID: "com.example.App"},
		{Type: "app.stop", BundleID: "com.example.Stop", GraceSeconds: constants.MaxGraceSeconds},
		{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo", WorkingDir: t.TempDir(), Env: map[string]string{"OK_KEY": "1"}},
		{Type: "process.stop", ProcessID: "worker-2", GraceSeconds: constants.MinGraceSeconds},
		{Type: "command.run", Executable: "/bin/echo", TimeoutSeconds: constants.MinActionTimeoutSeconds},
		{Type: "notify", Title: "title", Message: "message", Level: "info"},
	}}}
	if _, errs := Validate(&cfg); len(errs) != 0 {
		t.Fatalf("valid actions rejected: %#v", errs)
	}

	cases := []struct {
		name   string
		mutate func(*Config)
		code   string
	}{
		{"missing executable", func(c *Config) { c.TaskSource.Executable = "" }, codeMissingRequiredField},
		{"relative executable", func(c *Config) { c.TaskSource.Executable = "echo" }, codeExecutableNotFound},
		{"bad working directory", func(c *Config) { c.Rules[0].Ensure[2].WorkingDir = "relative" }, codeValueOutOfRange},
		{"shell denied", func(c *Config) {
			c.Rules[0].Ensure = []Action{{Type: "command.run", Executable: "/bin/echo", Shell: true}}
		}, codeShellNotAllowed},
		{"bad notify", func(c *Config) { c.Rules[0].Ensure = []Action{{Type: "notify", Title: "", Message: "x"}} }, codeMissingRequiredField},
		{"unsupported action", func(c *Config) { c.Rules[0].Ensure = []Action{{Type: "browser.redirect"}} }, codeUnsupportedAction},
		{"bad app id", func(c *Config) { c.Rules[0].Ensure = []Action{{Type: "app.start", BundleID: "!"}} }, codeInvalidIdentifier},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := cfg
			copy.Rules = append([]Rule(nil), cfg.Rules...)
			copy.Rules[0].Ensure = append([]Action(nil), cfg.Rules[0].Ensure...)
			tc.mutate(&copy)
			_, errs := Validate(&copy)
			found := false
			for _, err := range errs {
				if err.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("errors=%#v, want %s", errs, tc.code)
			}
		})
	}
}

func TestLoadAndPermissionsSuccessAndFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	good := "version: 2\ntask_source:\n  executable: /bin/echo\n"
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, errs, err := Load(path); err != nil || cfg == nil || len(errs) != 0 {
		t.Fatalf("Load success = %#v %#v %v", cfg, errs, err)
	}
	if _, err := CheckPermissions(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing error = %v", err)
	}
	if _, err := DefaultPath(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDuplicateConflictAndRangeErrors(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	cfg.Polling.IntervalSeconds = constants.MinPollIntervalSeconds
	cfg.Polling.TimeoutSeconds = constants.MaxPollTimeoutSeconds
	cfg.Rules = []Rule{
		{ID: "same", Priority: 3, Ensure: []Action{{Type: "process.start", ProcessID: "p", Executable: "/bin/echo"}, {Type: "app.start", BundleID: "com.x"}}},
		{ID: "same", Priority: 3, Ensure: []Action{{Type: "process.start", ProcessID: "p", Executable: "/bin/echo"}, {Type: "app.stop", BundleID: "com.x"}}},
	}
	_, errs := Validate(&cfg)
	if len(errs) < 4 {
		t.Fatalf("errors=%#v", errs)
	}
	if (ValidationError{Code: "x", Path: "y"}).Error() != "x: y" {
		t.Fatal("error format")
	}
}

func TestLoadRejectsUnknownAndUnsafeShellConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("version: 2\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errs, err := Load(path); err != nil || len(errs) != 1 || errs[0].Code != codeUnknownField {
		t.Fatalf("unknown field=%#v %v", errs, err)
	}
	if err := os.WriteFile(path, []byte("version: 2\ntask_source:\n  executable: /bin/echo\nsafety:\n  allow_shell: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); !errors.Is(err, ErrInsecurePermissions) {
		t.Fatalf("shell mode error=%v", err)
	}
}

func TestLoadShellConfigWithPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("version: 2\ntask_source:\n  executable: /bin/echo\nsafety:\n  allow_shell: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, errs, err := Load(path); err != nil || cfg == nil || len(errs) != 0 {
		t.Fatalf("private shell config = %#v %#v %v", cfg, errs, err)
	}
}

func TestCheckPermissionsRejectsDirectory(t *testing.T) {
	if _, err := CheckPermissions(t.TempDir()); !errors.Is(err, ErrInsecurePermissions) {
		t.Fatalf("directory error = %v", err)
	}
}

func TestValidateRejectsRemainingPolicyValues(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	cfg.Polling.FailurePolicy = "stop"
	cfg.Logging.RetainDays = 0
	cfg.Rules = []Rule{{ID: "r", Match: Match{TaskNameContains: []string{""}}, Ensure: []Action{
		{Type: "notify", Title: "title", Message: "message", Level: "debug"},
		{Type: "process.stop", ProcessID: ""},
	}}}
	_, errs := Validate(&cfg)
	if len(errs) < 4 {
		t.Fatalf("remaining policy errors = %#v", errs)
	}
}
