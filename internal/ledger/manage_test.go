//go:build !windows

package ledger

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestManagerStartAndStop_RoundTrip(t *testing.T) {
	ledger := &Ledger{Version: 1, Entries: map[string]Entry{}, path: filepath.Join(t.TempDir(), "ledger.json")}
	manager := NewManager(ledger)
	ctx := context.Background()
	spec := StartSpec{
		ProcessID:  "task",
		Executable: "/bin/sleep",
		Args:       []string{"5"},
	}

	if status, err := manager.Start(ctx, spec, false); err != nil || status != "started" {
		t.Fatalf("start status=%q err=%v", status, err)
	}
	if status, err := manager.Start(ctx, spec, false); err != nil || status != "already_running" {
		t.Fatalf("expected already_running on second start, got=%q err=%v", status, err)
	}

	stop, err := manager.Stop(ctx, "task", 200*time.Millisecond, false)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stop.Status != "stopped" {
		t.Fatalf("unexpected stop status=%q", stop.Status)
	}
}

func TestManagerStop_NormalTermination_OneSignal(t *testing.T) {
	ledger := &Ledger{Version: 1, Entries: map[string]Entry{}, path: filepath.Join(t.TempDir(), "ledger.json")}
	manager := NewManager(ledger)

	status, err := manager.Start(context.Background(), StartSpec{
		ProcessID:  "task",
		Executable: "/bin/sleep",
		Args:       []string{"5"},
	}, false)
	if err != nil || status != "started" {
		t.Fatalf("start status=%q err=%v", status, err)
	}

	stop, err := manager.Stop(context.Background(), "task", 200*time.Millisecond, false)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stop.Status != "stopped" {
		t.Fatalf("status=%q, want stopped", stop.Status)
	}
	if stop.SignalsSent != 1 {
		t.Fatalf("signals=%d, want 1", stop.SignalsSent)
	}
}

func TestManagerStop_Refused_NoRetry(t *testing.T) {
	ledger := &Ledger{Version: 1, Entries: map[string]Entry{}, path: filepath.Join(t.TempDir(), "ledger.json")}
	manager := NewManager(ledger)
	ctx := context.Background()

	ready := filepath.Join(t.TempDir(), "stubborn-ready")
	helper := exec.Command(os.Args[0], "-test.run=TestManagerStop_RefusedHelperProcess")
	helper.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "GO_HELPER_READY_PATH="+ready)
	spec := StartSpec{
		ProcessID:  "stubborn",
		Executable: helper.Path,
		Args:       []string{"-test.run=TestManagerStop_RefusedHelperProcess"},
		Env:        helper.Env,
	}
	if status, err := manager.Start(ctx, spec, false); err != nil || status != "started" {
		t.Fatalf("start status=%q err=%v", status, err)
	}
	// Why: Wait until the helper ignores SIGTERM before testing the refused path.
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat helper readiness: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	stop, err := manager.Stop(ctx, "stubborn", 50*time.Millisecond, false)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stop.Status != "refused" {
		t.Fatalf("status=%q, want refused", stop.Status)
	}
	if stop.SignalsSent != 1 {
		t.Fatalf("signals=%d, want 1", stop.SignalsSent)
	}

	if _, ok := manager.Ledger.Get("stubborn"); !ok {
		t.Fatalf("entry should remain for refused stop")
	}
	t.Cleanup(func() {
		if value, ok := manager.Ledger.Get("stubborn"); ok {
			if proc, err := os.FindProcess(value.PID); err == nil {
				_ = proc.Kill()
			}
		}
	})
}

func TestManagerStop_RefusedHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		t.Skip("not a helper")
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	ready := os.Getenv("GO_HELPER_READY_PATH")
	if ready == "" {
		t.Fatal("missing helper readiness path")
	}
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		t.Fatalf("write helper readiness: %v", err)
	}
	<-ch
	select {}
}

func TestManagerStop_ArgsMismatch_NoSignal(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "ps")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env sh\nprintf 'Mon  Jan 01 00:00:00 2000 /usr/bin/sleep 10\\n'\n"), 0o700); err != nil {
		t.Fatalf("write fake ps: %v", err)
	}
	oldPsPath := psPath
	psPath = fake
	t.Cleanup(func() { psPath = oldPsPath })

	ledger := &Ledger{
		Version: 1,
		Entries: map[string]Entry{
			"task": {
				ProcessID: "task",
				PID:       123456,
				PSLstart:  "Mon Jan 01 00:00:00 2000",
				PSArgs:    "sleep 10",
			},
		},
		path: filepath.Join(t.TempDir(), "ledger.json"),
	}
	manager := NewManager(ledger)
	result, err := manager.Stop(context.Background(), "task", 100*time.Millisecond, false)
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

func TestStop_ProcessGone_NoSignal(t *testing.T) {
	oldPsPath := psPath
	workDir := t.TempDir()
	fake := filepath.Join(workDir, "ps")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env sh\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake ps: %v", err)
	}
	psPath = fake
	t.Cleanup(func() { psPath = oldPsPath })

	ledger := &Ledger{Version: 1, Entries: map[string]Entry{
		"task": {ProcessID: "task", PID: 999999, PSLstart: "START", PSArgs: "ARGS"},
	}, path: filepath.Join(workDir, "ledger.json")}
	manager := NewManager(ledger)
	result, err := manager.Stop(context.Background(), "task", time.Millisecond*10, false)
	if err != nil {
		t.Fatalf("Stop returned err=%v", err)
	}
	if result.SignalsSent != 0 {
		t.Fatalf("signals=%d, want 0", result.SignalsSent)
	}
	if result.Status != "orphan_dropped" {
		t.Fatalf("status=%q, want orphan_dropped", result.Status)
	}
}

func TestManagerStart_AlreadyRunning_NoDuplicate(t *testing.T) {
	ledger := &Ledger{Version: 1, Entries: map[string]Entry{}, path: filepath.Join(t.TempDir(), "ledger.json")}
	manager := NewManager(ledger)
	ctx := context.Background()
	spec := StartSpec{
		ProcessID:  "task",
		Executable: "/bin/sleep",
		Args:       []string{"5"},
	}
	status1, err := manager.Start(ctx, spec, false)
	if err != nil || status1 != "started" {
		t.Fatalf("first start status=%q err=%v", status1, err)
	}

	status2, err := manager.Start(ctx, spec, false)
	if err != nil {
		t.Fatalf("second start err=%v", err)
	}
	if status2 != "already_running" {
		t.Fatalf("second start status=%q, want already_running", status2)
	}

	stop, err := manager.Stop(ctx, "task", 200*time.Millisecond, false)
	if err != nil {
		t.Fatalf("stop=%v", err)
	}
	if stop.Status != "stopped" {
		t.Fatalf("stop status=%q, want stopped", stop.Status)
	}
	if stop.SignalsSent != 1 {
		t.Fatalf("signals=%d, want 1", stop.SignalsSent)
	}
}
