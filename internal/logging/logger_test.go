package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogWritesRedactedSingleLineJSON(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, "info")
	logger.Log(Entry{Level: "info", Component: "engine", Event: "failure", Message: "user@example.com Bearer token Token expires at tomorrow"})
	if bytes.Contains(output.Bytes(), []byte("@example.com")) || bytes.Contains(output.Bytes(), []byte("token")) || bytes.Contains(output.Bytes(), []byte("Token expires")) {
		t.Fatalf("secret leaked: %q", output.String())
	}
	if lines := bytes.Count(output.Bytes(), []byte("\n")); lines != 1 {
		t.Fatalf("lines = %d, want 1", lines)
	}
	var entry Entry
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &entry); err != nil {
		t.Fatal(err)
	}
}

func TestLogFiltersBelowThreshold(t *testing.T) {
	var output bytes.Buffer
	New(&output, "info").Log(Entry{Level: "debug"})
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty", output.String())
	}
}

func TestRotateRemovesOnlyExpiredConnectorLogs(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "backend.log.1")
	keep := filepath.Join(dir, "backend.log")
	other := filepath.Join(dir, "other.log")
	for _, path := range []string{old, keep, other} {
		if err := os.WriteFile(path, []byte("log"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(old, now.AddDate(0, 0, -2), now.AddDate(0, 0, -2)); err != nil {
		t.Fatal(err)
	}
	New(nil, "info").Rotate(dir, 1, now)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expired log still exists: %v", err)
	}
	for _, path := range []string{keep, other} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
}
