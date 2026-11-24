package config

import (
	"os"
	"path/filepath"
	"testing"
)

// description is a reader's note; it must load instead of failing as an unknown field.
func TestLoad_RuleDescription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	body := "version: 2\ntask_source:\n  executable: /bin/echo\nrules:\n- id: r\n  description: 説明\n  ensure:\n  - type: browser.block\n    domains: [x.com]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, errs, err := Load(path)
	if err != nil || len(errs) > 0 {
		t.Fatalf("load failed: err=%v validation=%v", err, errs)
	}
	if cfg.Rules[0].Description != "説明" {
		t.Fatalf("description = %q", cfg.Rules[0].Description)
	}
}
