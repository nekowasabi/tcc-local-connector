package ledger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_ValidAndVersionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")

	valid := `{"version":1,"entries":{"task":{"process_id":"task","pid":12345,"ps_lstart":"START","ps_args":"ARGS"}}}`
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatalf("write valid ledger: %v", err)
	}
	ledger, err := Load(path)
	if err != nil {
		t.Fatalf("Load valid: %v", err)
	}
	if ledger.Version != 1 || len(ledger.Entries) != 1 {
		t.Fatalf("unexpected valid load: %+v", ledger)
	}

	mismatch := `{"version":2,"entries":{"task":{"process_id":"task","pid":12345,"ps_lstart":"START","ps_args":"ARGS"}}}`
	if err := os.WriteFile(path, []byte(mismatch), 0o600); err != nil {
		t.Fatalf("write version mismatch ledger: %v", err)
	}
	ledger, err = Load(path)
	if err != nil {
		t.Fatalf("Load version mismatch: %v", err)
	}
	if ledger.Version != 1 || len(ledger.Entries) != 0 {
		t.Fatalf("version mismatch should reset ledger: %+v", ledger)
	}
}

func TestLedger_PutGetAll_Remove(t *testing.T) {
	ledger := &Ledger{Version: 1, Entries: map[string]Entry{}, path: filepath.Join(t.TempDir(), "ledger.json")}
	ledger.Put(Entry{ProcessID: "task", PID: 123, PSLstart: "START", PSArgs: "ARGS"})
	if _, ok := ledger.Get("task"); !ok {
		t.Fatalf("expected put entry")
	}

	entries := ledger.All()
	if len(entries) != 1 {
		t.Fatalf("expected all entries=1, got=%d", len(entries))
	}

	ledger.Remove("task")
	if _, ok := ledger.Get("task"); ok {
		t.Fatalf("expected removed entry")
	}
}

func TestManagerStop_MissingEntry(t *testing.T) {
	ledger := &Ledger{Version: 1, Entries: map[string]Entry{}, path: filepath.Join(t.TempDir(), "ledger.json")}
	manager := NewManager(ledger)
	result, err := manager.Stop(context.Background(), "missing", 200*time.Millisecond, false)
	if err != nil {
		t.Fatalf("stop missing entry: %v", err)
	}
	if result.Status != "orphan_dropped" {
		t.Fatalf("unexpected status=%q", result.Status)
	}
}
