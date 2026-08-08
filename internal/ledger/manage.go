package ledger

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

type StartSpec struct {
	ProcessID  string
	Executable string
	Args       []string
	WorkingDir string
	Env        []string
}
type StopResult struct {
	Status      string `json:"status"`
	SignalsSent int    `json:"signals_sent"`
}
type Manager struct{ Ledger *Ledger }

func NewManager(ledger *Ledger) *Manager { return &Manager{Ledger: ledger} }
func (m *Manager) Start(ctx context.Context, spec StartSpec, dryRun bool) (string, error) {
	if dryRun {
		return "skipped", nil
	}
	if entry, ok := m.Ledger.Get(spec.ProcessID); ok {
		live, err := VerifyEntry(ctx, entry)
		if err == nil && live {
			return "already_running", nil
		}
		m.Ledger.Remove(spec.ProcessID)
	}
	cmd := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	cmd.Dir = spec.WorkingDir
	cmd.Env = spec.Env
	if err := cmd.Start(); err != nil {
		return "", err
	}
	start, args, live, err := processIdentity(ctx, cmd.Process.Pid)
	if err != nil || !live {
		return "", err
	}
	entry := Entry{ProcessID: spec.ProcessID, PID: cmd.Process.Pid, PSLstart: start, PSArgs: args}
	m.Ledger.Put(entry)
	return "started", m.Ledger.Save()
}
func (m *Manager) Stop(ctx context.Context, id string, grace time.Duration, dryRun bool) (StopResult, error) {
	if dryRun {
		return StopResult{Status: "skipped"}, nil
	}
	entry, ok := m.Ledger.Get(id)
	if !ok {
		return StopResult{Status: "orphan_dropped"}, nil
	}
	live, err := VerifyEntry(ctx, entry)
	if err != nil {
		return StopResult{Status: "refused"}, err
	}
	if !live {
		m.Ledger.Remove(id)
		return StopResult{Status: "orphan_dropped"}, m.Ledger.Save()
	}
	if err := syscall.Kill(entry.PID, syscall.SIGTERM); err != nil {
		return StopResult{Status: "refused"}, err
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return StopResult{Status: "refused", SignalsSent: 1}, ctx.Err()
	case <-timer.C:
	}
	live, err = VerifyEntry(ctx, entry)
	if err != nil {
		return StopResult{Status: "refused", SignalsSent: 1}, err
	}
	if live {
		return StopResult{Status: "refused", SignalsSent: 1}, nil
	}
	m.Ledger.Remove(id)
	return StopResult{Status: "stopped", SignalsSent: 1}, m.Ledger.Save()
}
func (m *Manager) Reconcile(ctx context.Context) (int, error) {
	removed := 0
	for _, entry := range m.Ledger.All() {
		live, err := VerifyEntry(ctx, entry)
		if err != nil {
			return removed, err
		}
		if !live {
			m.Ledger.Remove(entry.ProcessID)
			removed++
		}
	}
	if removed > 0 {
		return removed, m.Ledger.Save()
	}
	return removed, nil
}
