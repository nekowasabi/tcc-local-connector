package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takets/tcc-local-connector/internal/constants"
)

func writeIdleGuardConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIdleGuardOmittedUsesDefaultsDisabled(t *testing.T) {
	path := writeIdleGuardConfig(t, "version: 2\ntask_source:\n  executable: /bin/echo\n")
	cfg, errs, err := Load(path)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Load = %#v %#v %v", cfg, errs, err)
	}
	if cfg.IdleGuard.Enabled {
		t.Fatalf("enabled = true, want false")
	}
	if cfg.IdleGuard.IdleMinutes != constants.DefaultIdleGuardIdleMinutes {
		t.Fatalf("idle_minutes = %d", cfg.IdleGuard.IdleMinutes)
	}
	if cfg.IdleGuard.GraceSeconds != constants.DefaultIdleGuardGraceSeconds {
		t.Fatalf("grace_seconds = %d", cfg.IdleGuard.GraceSeconds)
	}
	if len(cfg.IdleGuard.ExcludeApps) == 0 {
		t.Fatalf("exclude_apps is empty")
	}
}

func TestIdleGuardEnabledLoads(t *testing.T) {
	path := writeIdleGuardConfig(t, "version: 2\ntask_source:\n  executable: /bin/echo\nidle_guard:\n  enabled: true\n  idle_minutes: 15\n  grace_seconds: 60\n  exclude_apps: [Finder]\n")
	cfg, errs, err := Load(path)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Load = %#v %#v %v", cfg, errs, err)
	}
	if !cfg.IdleGuard.Enabled || cfg.IdleGuard.IdleMinutes != 15 || len(cfg.IdleGuard.ExcludeApps) != 1 {
		t.Fatalf("idle_guard = %#v", cfg.IdleGuard)
	}
}

func TestIdleGuardRejectsBadValues(t *testing.T) {
	cases := []struct {
		name string
		body string
		path string
	}{
		{"zero minutes", "version: 2\ntask_source:\n  executable: /bin/echo\nidle_guard:\n  enabled: true\n  idle_minutes: 0\n", "idle_guard.idle_minutes"},
		{"negative grace", "version: 2\ntask_source:\n  executable: /bin/echo\nidle_guard:\n  enabled: true\n  idle_minutes: 15\n  grace_seconds: -1\n", "idle_guard.grace_seconds"},
		{"empty exclude", "version: 2\ntask_source:\n  executable: /bin/echo\nidle_guard:\n  enabled: true\n  idle_minutes: 15\n  exclude_apps: ['']\n", "idle_guard.exclude_apps[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeIdleGuardConfig(t, tc.body)
			_, errs, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, validationErr := range errs {
				if validationErr.Code == codeValueOutOfRange && validationErr.Path == tc.path {
					found = true
				}
			}
			if !found {
				t.Fatalf("errors=%#v, want %s at %s", errs, codeValueOutOfRange, tc.path)
			}
		})
	}
}

func TestIdleGuardRejectsUnknownField(t *testing.T) {
	path := writeIdleGuardConfig(t, "version: 2\ntask_source:\n  executable: /bin/echo\nidle_guard:\n  bogus: true\n")
	_, errs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 || errs[0].Code != codeUnknownField {
		t.Fatalf("errors=%#v", errs)
	}
}

func TestIdleGuardGraceZeroAccepted(t *testing.T) {
	path := writeIdleGuardConfig(t, "version: 2\ntask_source:\n  executable: /bin/echo\nidle_guard:\n  enabled: true\n  idle_minutes: 15\n  grace_seconds: 0\n")
	cfg, errs, err := Load(path)
	if err != nil || len(errs) != 0 || cfg == nil {
		t.Fatalf("Load = %#v %#v %v", cfg, errs, err)
	}
}

func TestIdleGuardZeroStructNormalizesToDefaults(t *testing.T) {
	cfg := defaults()
	cfg.TaskSource.Executable = "/bin/echo"
	cfg.IdleGuard = IdleGuard{}
	if _, errs := Validate(&cfg); len(errs) != 0 {
		t.Fatalf("errors=%#v", errs)
	}
	if cfg.IdleGuard.Enabled || cfg.IdleGuard.IdleMinutes != constants.DefaultIdleGuardIdleMinutes || cfg.IdleGuard.GraceSeconds != constants.DefaultIdleGuardGraceSeconds || len(cfg.IdleGuard.ExcludeApps) == 0 {
		t.Fatalf("idle_guard = %#v", cfg.IdleGuard)
	}
}
