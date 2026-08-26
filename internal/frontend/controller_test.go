package frontend

import (
	"testing"
	"time"
)

type mockBackend struct {
	state         State
	sent          []string
	params        []any
	plans         []PlanPayload
	responses     []Response
	notifications []NotifyPayload
}

func (m *mockBackend) State() State { return m.state }
func (m *mockBackend) Launch(string, []string) error {
	m.state = StateRunning
	return nil
}
func (m *mockBackend) Send(method string, params any) error {
	m.sent = append(m.sent, method)
	m.params = append(m.params, params)
	return nil
}
func (m *mockBackend) TakeResponses() []Response {
	out := m.responses
	m.responses = nil
	return out
}
func (m *mockBackend) TakePlans() []PlanPayload {
	out := m.plans
	m.plans = nil
	return out
}
func (m *mockBackend) TakeNotifications() []NotifyPayload {
	out := m.notifications
	m.notifications = nil
	return out
}
func (m *mockBackend) Shutdown() { m.state = StateTerminated }

type mockSystem struct {
	opened     []string
	terminated bool
}

func (m *mockSystem) Open(path string) error {
	m.opened = append(m.opened, path)
	return nil
}
func (m *mockSystem) Terminate() { m.terminated = true }

func TestControllerMenuActionsReachBackendOrSystem(t *testing.T) {
	backend := &mockBackend{state: StateRunning}
	system := &mockSystem{}
	controller := NewController(backend, system)
	controller.snapshot.Paths = map[string]string{
		"config": `C:\tmp\config.yml`,
		"log":    `C:\tmp\backend.log`,
	}

	for _, action := range []Action{
		ActionRefresh,
		ActionReloadConfig,
		ActionOpenConfig,
		ActionOpenLog,
		ActionPauseShort,
		ActionPauseLong,
		ActionPauseUntilNextStart,
		ActionResume,
		ActionShowStatus,
	} {
		controller.Perform(action)
	}
	controller.Perform(ActionQuit)

	if !contains(backend.sent, "refresh_now") || !contains(backend.sent, "reload_config") || !contains(backend.sent, "pause") || !contains(backend.sent, "resume") {
		t.Fatalf("sent = %#v", backend.sent)
	}
	if !contains(backend.sent, "config_paths") || !contains(backend.sent, "status") {
		t.Fatalf("missing poll methods: %#v", backend.sent)
	}
	if len(system.opened) != 2 || system.opened[0] != `C:\tmp\config.yml` || system.opened[1] != `C:\tmp\backend.log` {
		t.Fatalf("opened = %#v", system.opened)
	}
	if !system.terminated {
		t.Fatal("system was not terminated")
	}
}

func TestControllerReportsPlansAsSkipped(t *testing.T) {
	backend := &mockBackend{state: StateRunning, plans: []PlanPayload{{
		CycleID: 42,
		Actions: []PlanAction{{ActionID: "42-1", Kind: "notify"}},
	}}}
	controller := NewController(backend, &mockSystem{})
	controller.ConsumePending()
	if len(backend.sent) != 1 || backend.sent[0] != "report_actions" {
		t.Fatalf("sent = %#v", backend.sent)
	}
	params, _ := backend.params[0].(map[string]any)
	if params["cycle_id"] != int64(42) {
		t.Fatalf("params = %#v", params)
	}
}

func TestControllerUpdatesVisibleState(t *testing.T) {
	backend := &mockBackend{
		state: StateRunning,
		responses: []Response{{
			ID: "status",
			Result: map[string]any{
				"state":         "active",
				"cycle_id":      float64(7),
				"parse_ok":      true,
				"running_tasks": []any{map[string]any{"name": "執筆", "task_id": "task_1"}},
			},
		}},
		notifications: []NotifyPayload{{Code: "notice", Message: "warning"}},
	}
	controller := NewController(backend, &mockSystem{})
	controller.ConsumePending()
	snap := controller.Snapshot()
	if snap.TaskName != "執筆" || snap.Warning != "warning" {
		t.Fatalf("snapshot = %#v", snap)
	}
}

func TestNextDayStartUsesConfiguredHour(t *testing.T) {
	now := time.Date(2026, 8, 26, 15, 29, 0, 0, time.FixedZone("JST", 9*3600))
	got := nextDayStart(now, 5)
	if got.Hour() != 5 || got.Day() != 27 {
		t.Fatalf("next day start = %s", got)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
