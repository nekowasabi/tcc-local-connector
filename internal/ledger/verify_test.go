package ledger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyEntryAcceptsCurrentProcessIdentity(t *testing.T) {
	start, args, live, err := processIdentity(context.Background(), os.Getpid())
	if err != nil || !live {
		t.Fatalf("processIdentity() = (%q, %q, %v, %v)", start, args, live, err)
	}
	ok, err := VerifyEntry(context.Background(), Entry{PID: os.Getpid(), PSLstart: start, PSArgs: args})
	if err != nil || !ok {
		t.Fatalf("VerifyEntry() = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestStop_PIDReused(t *testing.T) {
	oldPsPath := psPath
	workDir := t.TempDir()
	fake := filepath.Join(workDir, "ps")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake ps: %v", err)
	}
	psPath = fake
	t.Cleanup(func() { psPath = oldPsPath })

	ledger := &Ledger{Entries: map[string]Entry{
		"task": {PID: 999999, PSLstart: "START", PSArgs: "ARGS"},
	}, path: filepath.Join(workDir, "ledger.json")}
	manager := NewManager(ledger)
	result, err := manager.Stop(context.Background(), "task", time.Millisecond*10, false)
	if err != nil {
		t.Fatalf("Stop returned err=%v", err)
	}
	t.Logf("signals_sent=%d status=%s", result.SignalsSent, result.Status)
	if result.SignalsSent != 0 {
		t.Fatalf("signals=%d, want 0", result.SignalsSent)
	}
	if result.Status != "orphan_dropped" {
		t.Fatalf("status=%q, want orphan_dropped", result.Status)
	}
}

func TestVerifyEntry_PsExecFailure_ReturnsFalseErr(t *testing.T) {
	oldPsPath := psPath
	workDir := t.TempDir()
	invalid := filepath.Join(workDir, "missing-ps")
	if err := os.WriteFile(invalid, []byte(""), 0o600); err != nil {
		t.Fatalf("write placeholder failed: %v", err)
	}
	psPath = invalid + "-does-not-exist"
	t.Cleanup(func() { psPath = oldPsPath })

	ok, err := VerifyEntry(context.Background(), Entry{PID: os.Getpid(), PSLstart: "", PSArgs: ""})
	if ok {
		t.Fatalf("expected false for ps execution failure")
	}
	if err == nil {
		t.Fatalf("expected error for ps execution failure")
	}
}
