package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takets/tcc-local-connector/internal/constants"
)

func TestValidationErrorCodes(t *testing.T) {
	cases := []struct {
		name       string
		code       string
		validateFn func(t *testing.T) []ValidationError
	}{
		{
			name: "unsupported_config_version",
			code: codeUnsupportedConfigVersion,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Version = 99
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "missing_required_field",
			code: codeMissingRequiredField,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.TaskSource.Executable = ""
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "unknown_field",
			code: codeUnknownField,
			validateFn: func(t *testing.T) []ValidationError {
				path := filepath.Join(t.TempDir(), "config.yml")
				if err := os.WriteFile(path, []byte("version: 2\ntask_source:\n  executable: /bin/echo\nunknown: true\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, errs, err := Load(path)
				if err != nil {
					t.Fatalf("expected validation path, got %v", err)
				}
				return errs
			},
		},
		{
			name: "unknown_action_type",
			code: codeUnknownActionType,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Rules = []Rule{{ID: "rule", Ensure: []Action{{Type: "unknown"}}}}
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "unsupported_action",
			code: codeUnsupportedAction,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Rules = []Rule{{ID: "rule", Ensure: []Action{{Type: "browser.redirect"}}}}
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "duplicate_rule_id",
			code: codeDuplicateRuleID,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Rules = []Rule{{ID: "dup"}, {ID: "dup"}}
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "duplicate_process_id",
			code: codeDuplicateProcessID,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Rules = []Rule{
					{ID: "r1", Ensure: []Action{{Type: "process.start", ProcessID: "same", Executable: "/bin/echo"}}},
					{ID: "r2", Ensure: []Action{{Type: "process.start", ProcessID: "same", Executable: "/bin/echo"}}},
				}
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "value_out_of_range",
			code: codeValueOutOfRange,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Polling.IntervalSeconds = constants.MaxPollIntervalSeconds + 1
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "timeout_exceeds_interval",
			code: codeTimeoutExceedsInterval,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Polling.TimeoutSeconds = cfg.Polling.IntervalSeconds
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "unsupported_value",
			code: codeUnsupportedValue,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Polling.FailurePolicy = "never"
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "executable_not_found",
			code: codeExecutableNotFound,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.TaskSource.Executable = "/does/not/exist"
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "invalid_identifier",
			code: codeInvalidIdentifier,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Rules = []Rule{{ID: "bad id!", Ensure: []Action{{Type: "app.start", BundleID: "com.example.app"}}}}
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "shell_not_allowed",
			code: codeShellNotAllowed,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Rules = []Rule{{ID: "r", Ensure: []Action{{Type: "command.run", Executable: "/bin/echo", Shell: true}}}}
				cfg.Safety.AllowShell = false
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "forbidden_safety_flag",
			code: codeForbiddenSafetyFlag,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Safety.AllowForceTerminate = true
				_, errs := Validate(&cfg)
				return errs
			},
		},
		{
			name: "rule_conflict_same_priority",
			code: codeRuleConflictSamePriority,
			validateFn: func(t *testing.T) []ValidationError {
				cfg := defaults()
				cfg.Rules = []Rule{
					{ID: "a", Priority: 10, Ensure: []Action{{Type: "app.start", BundleID: "com.example.app"}}},
					{ID: "b", Priority: 10, Ensure: []Action{{Type: "app.stop", BundleID: "com.example.app"}}},
				}
				_, errs := Validate(&cfg)
				return errs
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			has := false
			for _, err := range tc.validateFn(t) {
				if err.Code == tc.code {
					has = true
					break
				}
			}
			if !has {
				t.Fatalf("missing expected code %q", tc.code)
			}
		})
	}
}

func TestLoadInvalidReturnsNilConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(cfgPath, []byte("version: 2\ntask_source:\n  executable: /does/not/exist\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, errs, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("expected validation path, got transport error %v", err)
	}
	if cfg != nil {
		t.Fatalf("config must be nil, got %#v", cfg)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestReloadInvalidKeepsPrevious(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("version: 2\ntask_source:\n  executable: /bin/echo\nrules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, errs, err := Load(path)
	if err != nil || len(errs) != 0 || cfg == nil {
		t.Fatalf("initial load failed: cfg=%v errs=%v err=%v", cfg, errs, err)
	}
	if err := os.WriteFile(path, []byte("version: 2\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("expected load path not transport error: %v", err)
	}
	if reloaded != nil {
		t.Fatalf("expected nil config on invalid reload")
	}
}

func TestUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("version: 2\ntask_source:\n  executable: /bin/echo\nunknown_key: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errs, err := Load(path)
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if len(errs) != 1 || errs[0].Code != codeUnknownField {
		t.Fatalf("got errs=%#v", errs)
	}
}

func TestCheckPermissionsBranches(t *testing.T) {
	rootDir := t.TempDir()
	valid := filepath.Join(rootDir, "config.yml")
	if err := os.WriteFile(valid, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckPermissions(valid); err != nil {
		t.Fatalf("valid file must pass: %v", err)
	}

	t.Run("rejects_non_regular", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "dir")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckPermissions(dir); err == nil {
			t.Fatalf("directory must be rejected")
		}
	})

	t.Run("rejects_symlink", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "link.yml")
		if err := os.Symlink(valid, link); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckPermissions(link); err == nil {
			t.Fatalf("symlink must be rejected")
		}
	})

	t.Run("rejects_group_or_other_writable", func(t *testing.T) {
		unsafePath := filepath.Join(t.TempDir(), "unsafe.yml")
		if err := os.WriteFile(unsafePath, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(unsafePath, 0o666); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckPermissions(unsafePath); err == nil {
			t.Fatalf("unsafe mode must be rejected")
		}
	})

	t.Run("rejects_allow_shell_private_mode", func(t *testing.T) {
		privatePath := filepath.Join(t.TempDir(), "private.yml")
		if err := os.WriteFile(privatePath, []byte("version: 2\ntask_source:\n  executable: /bin/echo\nsafety:\n  allow_shell: true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load(privatePath); err == nil {
			t.Fatalf("allow_shell requires private mode")
		}
	})
}
