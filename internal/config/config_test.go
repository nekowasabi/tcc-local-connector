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

func TestValidateDefaultOnTaskStartNormalizesAndPreservesCompatibility(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	if _, errs := Validate(&cfg); len(errs) != 0 || cfg.Default.OnTaskStart == nil || len(cfg.Default.OnTaskStart) != 0 {
		t.Fatalf("undefined default = %#v, %#v", cfg.Default, errs)
	}

	cfg.Default.OnTaskStart = []Action{{Type: "command.run", Executable: "/bin/echo"}}
	if _, errs := Validate(&cfg); len(errs) != 0 {
		t.Fatalf("valid default = %#v", errs)
	}
	if got := cfg.Default.OnTaskStart[0].TimeoutSeconds; got != constants.DefaultActionTimeoutSeconds {
		t.Fatalf("default timeout = %d, want %d", got, constants.DefaultActionTimeoutSeconds)
	}
}

func TestValidateDefaultOnTaskStartRejectsUnsafeAndConflictingActions(t *testing.T) {
	cases := []struct {
		name   string
		action []Action
		code   string
	}{
		{name: "browser block", action: []Action{{Type: constants.BrowserBlockActionType}}, code: codeUnsupportedAction},
		{name: "timeout", action: []Action{{Type: "command.run", Executable: "/bin/echo", TimeoutSeconds: constants.MaxActionTimeoutSeconds + 1}}, code: codeValueOutOfRange},
		{name: "shell", action: []Action{{Type: "command.run", Executable: "/bin/echo", Shell: true}}, code: codeShellNotAllowed},
		{name: "app conflict", action: []Action{{Type: "app.start", BundleID: "com.example.App"}, {Type: "app.stop", BundleID: "com.example.App"}}, code: codeRuleConflictSamePriority},
		{name: "process conflict", action: []Action{{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"}, {Type: "process.stop", ProcessID: "worker"}}, code: codeRuleConflictSamePriority},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults()
			cfg.TaskSource.Executable = "/bin/echo"
			cfg.Default.OnTaskStart = tc.action
			_, errs := Validate(&cfg)
			if !hasValidationCode(errs, tc.code) {
				t.Fatalf("errors=%#v, want %s", errs, tc.code)
			}
		})
	}
}

func TestValidateDefaultOnTaskStartAllowsCrossPhaseProcessID(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	cfg.Default.OnTaskStart = []Action{{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"}}
	cfg.Rules = []Rule{{ID: "rule", Ensure: []Action{{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"}}}}
	if _, errs := Validate(&cfg); len(errs) != 0 {
		t.Fatalf("cross-phase process ID rejected: %#v", errs)
	}
}

func TestValidateStopOnTaskEndNormalizesAndPreservesCompatibility(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	if _, errs := Validate(&cfg); len(errs) != 0 || cfg.Stop.OnTaskEnd == nil || len(cfg.Stop.OnTaskEnd) != 0 {
		t.Fatalf("undefined stop = %#v, %#v", cfg.Stop, errs)
	}

	cfg.Stop.OnTaskEnd = []Action{{Type: "command.run", Executable: "/bin/echo"}}
	if _, errs := Validate(&cfg); len(errs) != 0 {
		t.Fatalf("valid stop = %#v", errs)
	}
	if got := cfg.Stop.OnTaskEnd[0].TimeoutSeconds; got != constants.DefaultActionTimeoutSeconds {
		t.Fatalf("stop timeout = %d, want %d", got, constants.DefaultActionTimeoutSeconds)
	}
}

func TestValidateStopOnTaskEndRejectsUnsafeAndConflictingActions(t *testing.T) {
	cases := []struct {
		name   string
		action []Action
		code   string
	}{
		{name: "browser block", action: []Action{{Type: constants.BrowserBlockActionType}}, code: codeUnsupportedAction},
		{name: "timeout", action: []Action{{Type: "command.run", Executable: "/bin/echo", TimeoutSeconds: constants.MaxActionTimeoutSeconds + 1}}, code: codeValueOutOfRange},
		{name: "shell", action: []Action{{Type: "command.run", Executable: "/bin/echo", Shell: true}}, code: codeShellNotAllowed},
		{name: "app conflict", action: []Action{{Type: "app.start", BundleID: "com.example.App"}, {Type: "app.stop", BundleID: "com.example.App"}}, code: codeRuleConflictSamePriority},
		{name: "process conflict", action: []Action{{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"}, {Type: "process.stop", ProcessID: "worker"}}, code: codeRuleConflictSamePriority},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults()
			cfg.TaskSource.Executable = "/bin/echo"
			cfg.Stop.OnTaskEnd = tc.action
			_, errs := Validate(&cfg)
			if !hasValidationCode(errs, tc.code) {
				t.Fatalf("errors=%#v, want %s", errs, tc.code)
			}
		})
	}
}

func TestValidateStopOnTaskEndAllowsCrossPhaseProcessID(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	cfg.Default.OnTaskStart = []Action{{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"}}
	cfg.Stop.OnTaskEnd = []Action{{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"}}
	cfg.Rules = []Rule{{ID: "rule", Ensure: []Action{{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"}}}}
	if _, errs := Validate(&cfg); len(errs) != 0 {
		t.Fatalf("cross-phase process ID rejected: %#v", errs)
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

func TestExampleConfigLoads(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "config.example.yml"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, errs, loadErr := Load(path)
	if loadErr != nil || cfg == nil || len(errs) != 0 {
		t.Fatalf("example Load = %#v %#v %v", cfg, errs, loadErr)
	}
	var startID string
	var stopIDs []string
	for _, action := range cfg.Rules[0].Ensure {
		if action.Type == "app.start" {
			startID = action.BundleID
		}
		if action.Type == "app.stop" {
			stopIDs = action.BundleIDs
		}
	}
	if startID == "" || len(stopIDs) < 2 {
		t.Fatalf("example actions start=%q stop=%#v", startID, stopIDs)
	}
	for _, id := range stopIDs {
		if id == startID {
			t.Fatalf("example app.stop overlaps app.start %q", startID)
		}
	}
}

func TestLoadAppStopBundleIDsList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	payload := []byte(`version: 2
task_source:
  executable: /bin/echo
rules:
  - id: stop-inv
    ensure:
      - type: app.stop
        bundle_ids:
          - com.tinyspeck.slackmacgap
          - com.amazon.Lassen
`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, errs, err := Load(path)
	if err != nil || cfg == nil || len(errs) != 0 {
		t.Fatalf("Load = %#v %#v %v", cfg, errs, err)
	}
	got := cfg.Rules[0].Ensure[0].BundleIDs
	want := []string{"com.tinyspeck.slackmacgap", "com.amazon.Lassen"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("bundle_ids=%#v", got)
	}
}

func TestLoadAppStopSingleBundleID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	payload := []byte(`version: 2
task_source:
  executable: /bin/echo
rules:
  - id: stop-one
    ensure:
      - type: app.stop
        bundle_id: com.tinyspeck.slackmacgap
`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, errs, err := Load(path)
	if err != nil || cfg == nil || len(errs) != 0 {
		t.Fatalf("Load = %#v %#v %v", cfg, errs, err)
	}
	action := cfg.Rules[0].Ensure[0]
	if action.BundleID != "com.tinyspeck.slackmacgap" {
		t.Fatalf("bundle_id=%q", action.BundleID)
	}
	if len(action.BundleIDs) != 1 || action.BundleIDs[0] != "com.tinyspeck.slackmacgap" {
		t.Fatalf("normalized bundle_ids=%#v", action.BundleIDs)
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
