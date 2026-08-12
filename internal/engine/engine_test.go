package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/takets/tcc-local-connector/internal/browserpolicy"
	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/constants"
	"github.com/takets/tcc-local-connector/internal/ledger"
	"github.com/takets/tcc-local-connector/internal/rules"
	"github.com/takets/tcc-local-connector/internal/state"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "tcc-engine-test-home-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

func TestNewRestoresActivePause(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := filepath.Join(home, constants.StateDirRelPath)
	if err := state.SavePause(filepath.Join(stateDir, constants.PauseFileName), state.Pause{Until: time.Now().Add(time.Hour), Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	engine := New(Deps{ConfigPath: filepath.Join(t.TempDir(), "config.yml")})
	if got := engine.Snapshot().State; got != state.StatePaused {
		t.Fatalf("state = %q, want %q", got, state.StatePaused)
	}
}

func TestMachineReleasesAfterFailureGraceWithoutSuccess(t *testing.T) {
	machine := state.NewMachine()
	now := time.Now()
	machine.Transition("failure", now, 0)
	transition := machine.Transition("grace_expired", now.Add(time.Nanosecond), 0)
	if !transition.ReleaseControls || machine.Current() != state.StateReleased {
		t.Fatalf("transition = %#v, state = %q", transition, machine.Current())
	}
}

func TestGraceBoundary(t *testing.T) {
	machine := state.NewMachine()
	now := time.Now()
	grace := time.Second
	machine.Transition("failure", now, grace)
	t.Run("just_under", func(t *testing.T) {
		if transition := machine.Transition("grace_expired", now.Add(grace), grace); transition.ReleaseControls {
			t.Fatal("controls released at the grace boundary")
		}
	})
	machine = state.NewMachine()
	machine.Transition("failure", now, grace)
	t.Run("just_over", func(t *testing.T) {
		if transition := machine.Transition("grace_expired", now.Add(grace+time.Nanosecond), grace); !transition.ReleaseControls {
			t.Fatal("controls were not released after the grace boundary")
		}
	})
}

func TestDetectWakeAfterDelayedTick(t *testing.T) {
	engine := New(Deps{})
	engine.cfg = &config.Config{Polling: config.Polling{IntervalSeconds: constants.MinPollIntervalSeconds}}
	now := time.Now()
	if engine.detectWake(now) {
		t.Fatal("first tick must not be treated as wake")
	}
	if !engine.detectWake(now.Add(time.Duration(constants.MinPollIntervalSeconds*constants.WakeReevaluateThresholdFactor+1) * time.Second)) {
		t.Fatal("late tick was not treated as wake")
	}
}

func TestEngine_TickStartsCycle(t *testing.T) {
	fake := createFakeMCPScript(t)
	engine := New(Deps{})
	engine.cfg = &config.Config{
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: fake, Args: []string{"mcp"}},
		// Why: Use the default poll timeout instead of the minimum so process-start latency under the race detector is not mistaken for a fetch failure.
		Polling: config.Polling{TimeoutSeconds: constants.DefaultPollTimeoutSeconds, FailureGraceSeconds: constants.DefaultFailureGraceSeconds, FailurePolicy: "release_controls", IntervalSeconds: constants.MinPollIntervalSeconds},
		Logging: config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
		Rules:   []config.Rule{},
	}
	engine.status.RunningTasks = []TaskView{{Name: "seed", TaskID: "task_1234567890abcdef1234567890abcdef", Date: "2026-08-07"}}
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatalf("RunCycleNow() = %v", err)
	}
	if got := engine.Snapshot().CycleID; got != 1 {
		t.Fatalf("cycle_id = %d, want 1", got)
	}
	if got := engine.Snapshot().State; got != state.StateActive {
		t.Fatalf("state = %q, want active", got)
	}
}

func TestEngine_DryRunEmitsNotificationForSkippedActions(t *testing.T) {
	fake := createFakeMCPScript(t)
	events := make(chan any, 1)
	engine := New(Deps{Emit: func(name string, data any) {
		if name == "notify" {
			events <- data
		}
	}})
	engine.cfg = &config.Config{
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: fake, Args: []string{"mcp"}},
		Polling:    config.Polling{TimeoutSeconds: constants.DefaultPollTimeoutSeconds, FailureGraceSeconds: constants.DefaultFailureGraceSeconds, IntervalSeconds: constants.MinPollIntervalSeconds},
		Safety:     config.Safety{DryRun: true},
		Rules: []config.Rule{{
			ID:    "dry-run-rule",
			Match: config.Match{},
			Ensure: []config.Action{{
				Type:     "app.stop",
				BundleID: "com.example.App",
			}},
		}},
	}

	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatalf("RunCycleNow() = %v", err)
	}

	data := <-events
	notification, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("notification type = %T, want map[string]any", data)
	}
	if got := notification["code"]; got != "dry_run_skipped" {
		t.Fatalf("notification code = %v, want dry_run_skipped", got)
	}
	if got := notification["message"]; got != "dry_run: 1件のアクションを実行せずスキップしました" {
		t.Fatalf("notification message = %v", got)
	}
}

func TestEngine_DryRunPublishesPlannedDomainsAndNotifiesOnChange(t *testing.T) {
	fake := createActiveFakeMCPScript(t)
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	notifications := make(chan map[string]any, 4)
	engine := New(Deps{BrowserPolicyPath: policyPath, Emit: func(name string, data any) {
		if name == "notify" {
			if notification, ok := data.(map[string]any); ok && notification["code"] == "browser_block_dry_run" {
				notifications <- notification
			}
		}
	}})
	engine.cfg = browserTestConfig(fake, true)
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	policy := readBrowserPolicy(t, policyPath)
	if policy.Enforce || !policy.DryRun || len(policy.Domains) != 0 || !reflect.DeepEqual(policy.PlannedDomains, []string{"example.com"}) {
		t.Fatalf("dry-run policy = %#v", policy)
	}
	if notification := <-notifications; notification["title"] != "ブラウザ遮断（dry-run）" {
		t.Fatalf("notification = %#v", notification)
	}
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case notification := <-notifications:
		t.Fatalf("duplicate notification = %#v", notification)
	default:
	}
}

func TestRunCycleNowPublishesEnforcedBrowserPolicy(t *testing.T) {
	fake := createActiveFakeMCPScript(t)
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	engine := New(Deps{BrowserPolicyPath: policyPath})
	engine.cfg = browserTestConfig(fake, false)
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	policy := readBrowserPolicy(t, policyPath)
	want := []string{"example.com"}
	if !policy.Enforce || policy.DryRun || !reflect.DeepEqual(policy.Domains, want) || !reflect.DeepEqual(policy.PlannedDomains, want) {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestEngine_ReleaseControlsPublishesEmptyPolicy(t *testing.T) {
	fake := createActiveFakeMCPScript(t)
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	engine := New(Deps{BrowserPolicyPath: policyPath})
	engine.cfg = browserTestConfig(fake, false)
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine.ReportActions(engine.Snapshot().CycleID, []ActionResult{{ActionID: "1-1", Status: "failed"}})
	policy := readBrowserPolicy(t, policyPath)
	if policy.Enforce || policy.DryRun || len(policy.Domains) != 0 || len(policy.PlannedDomains) != 0 {
		t.Fatalf("release-controls policy = %#v", policy)
	}
}

func TestEngine_PauseAndResumePublishEmptyPolicy(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	engine := New(Deps{ConfigPath: filepath.Join(t.TempDir(), "config.yml"), BrowserPolicyPath: policyPath})
	if err := engine.Pause(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	paused := readBrowserPolicy(t, policyPath)
	if paused.Enforce || len(paused.Domains) != 0 || len(paused.PlannedDomains) != 0 {
		t.Fatalf("paused policy = %#v", paused)
	}
	if err := engine.Resume(); err != nil {
		t.Fatal(err)
	}
	resumed := readBrowserPolicy(t, policyPath)
	if resumed.Generation <= paused.Generation || resumed.Enforce {
		t.Fatalf("resumed policy = %#v after %#v", resumed, paused)
	}
}

type failOncePolicyStore struct {
	store    *browserpolicy.Store
	failNext bool
}

type memoryPolicyStore struct{ generation uint64 }

func (s *memoryPolicyStore) Publish(enforce, dryRun bool, domains, planned []string) (browserpolicy.Policy, error) {
	s.generation++
	return browserpolicy.Policy{Version: constants.BrowserPolicyVersion, Generation: s.generation, Enforce: enforce, DryRun: dryRun, Domains: domains, PlannedDomains: planned, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

func (s *memoryPolicyStore) PublishEmpty() (browserpolicy.Policy, error) {
	return s.Publish(false, false, []string{}, []string{})
}

func (s *failOncePolicyStore) Publish(enforce, dryRun bool, domains, planned []string) (browserpolicy.Policy, error) {
	if s.failNext {
		s.failNext = false
		return browserpolicy.Policy{}, errors.New("injected publication failure")
	}
	return s.store.Publish(enforce, dryRun, domains, planned)
}

func (s *failOncePolicyStore) PublishEmpty() (browserpolicy.Policy, error) {
	return s.store.PublishEmpty()
}

func TestRunCycleNowPolicyWriteFailureFallsOpen(t *testing.T) {
	fake := createActiveFakeMCPScript(t)
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	store := &failOncePolicyStore{store: browserpolicy.NewStore(policyPath), failNext: true}
	engine := New(Deps{BrowserPolicyPath: policyPath, BrowserPolicyStore: store})
	engine.cfg = browserTestConfig(fake, false)
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	policy := readBrowserPolicy(t, policyPath)
	if policy.Enforce || len(policy.Domains) != 0 || len(policy.PlannedDomains) != 0 {
		t.Fatalf("write-failure policy = %#v", policy)
	}
}

func TestEngine_RefreshNow_StartsImmediateCycle(t *testing.T) {
	fake := createFakeMCPScript(t)
	engine := New(Deps{})
	engine.cfg = &config.Config{
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: fake, Args: []string{"mcp"}},
		// Why: Use the default poll timeout instead of the minimum so process-start latency under the race detector is not mistaken for a fetch failure.
		Polling: config.Polling{TimeoutSeconds: constants.DefaultPollTimeoutSeconds, FailureGraceSeconds: constants.DefaultFailureGraceSeconds, FailurePolicy: "release_controls", IntervalSeconds: constants.MinPollIntervalSeconds},
		Logging: config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
		Rules:   []config.Rule{},
	}
	first, err := engine.RunCycleNow(context.Background())
	if err != nil {
		t.Fatalf("first run = %v", err)
	}
	second, err := engine.RunCycleNow(context.Background())
	if err != nil {
		t.Fatalf("second run = %v", err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("cycle ids = %d, %d, want 1,2", first, second)
	}
}

func TestSingleFlightRefreshDuringCycle(t *testing.T) {
	engine := New(Deps{})
	engine.cfg = &config.Config{
		Polling:    config.Polling{TimeoutSeconds: 1, FailureGraceSeconds: 0},
		TaskSource: config.TaskSource{Executable: "/bin/sleep", Args: []string{"2"}},
	}
	done := make(chan struct{})
	go func() {
		_, _ = engine.RunCycleNow(context.Background())
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		engine.mu.Lock()
		running := engine.running
		engine.mu.Unlock()
		if running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cycle did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := engine.RunCycleNow(context.Background()); err == nil || err.Error() != "busy" {
		t.Fatalf("second cycle error = %v, want busy", err)
	}
	<-done
}

func TestRunCycleNow_DoesNotLeakMuOnErrorPath(t *testing.T) {
	engine := New(Deps{})
	if _, err := engine.RunCycleNow(context.Background()); err == nil {
		t.Fatal("RunCycleNow should fail without config")
	}

	done := make(chan struct{})
	go func() {
		_, _ = engine.RunCycleNow(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunCycleNow did not return after previous early return path")
	}
}

func TestExecuteBackendActionsDryRunSuppressesCommandFailure(t *testing.T) {
	events := make(chan string, 1)
	engine := New(Deps{Emit: func(name string, _ any) { events <- name }})
	engine.executeBackendActions(context.Background(), []rules.PlannedAction{{Kind: "command.run", Executable: "/not/a/command"}}, true, false)
	select {
	case name := <-events:
		t.Fatalf("unexpected event %q", name)
	default:
	}
}

func TestExecuteBackendActionsRefusesShellWithoutPermission(t *testing.T) {
	events := make(chan string, 1)
	engine := New(Deps{Emit: func(name string, _ any) { events <- name }})
	engine.executeBackendActions(context.Background(), []rules.PlannedAction{{Kind: "command.run", Shell: true, Executable: "/not/a/command"}}, false, false)
	if name := <-events; name != "notify" {
		t.Fatalf("event = %q, want notify", name)
	}
}

func TestEngine_PollInterval_DefaultAndConfigured(t *testing.T) {
	engine := New(Deps{})
	if got := engine.pollInterval(); got != constants.DefaultPollIntervalSeconds*time.Second {
		t.Fatalf("pollInterval() default = %v, want %v", got, constants.DefaultPollIntervalSeconds*time.Second)
	}

	engine.cfg = &config.Config{Polling: config.Polling{IntervalSeconds: 7}}
	if got := engine.pollInterval(); got != 7*time.Second {
		t.Fatalf("pollInterval() configured = %v, want %v", got, 7*time.Second)
	}

	engine.cfg.Polling.IntervalSeconds = 0
	if got := engine.pollInterval(); got != constants.DefaultPollIntervalSeconds*time.Second {
		t.Fatalf("pollInterval() fallback = %v, want %v", got, constants.DefaultPollIntervalSeconds*time.Second)
	}
}

func TestEngine_SetEventSinkAndEmit(t *testing.T) {
	engine := New(Deps{})
	events := make(chan string, 1)
	engine.SetEventSink(func(name string, _ any) {
		events <- name
	})

	engine.emitEvent("engine_set_event_sink", map[string]any{"ok": true})
	select {
	case got := <-events:
		if got != "engine_set_event_sink" {
			t.Fatalf("event = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("event was not emitted")
	}
}

func TestEngine_ReloadSuccessAndStateChangedEvent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yml")
	configYAML := []byte(`task_source:
  type: tcc2_mcp
  executable: /bin/echo
  args: ["mcp"]
`)
	if err := os.WriteFile(configPath, configYAML, 0o600); err != nil {
		t.Fatal(err)
	}

	engine := New(Deps{ConfigPath: configPath})
	events := make(chan string, 1)
	engine.SetEventSink(func(name string, _ any) {
		events <- name
	})

	result := engine.Reload()
	if !result.OK || !result.Applied {
		t.Fatalf("Reload() = %#v", result)
	}
	select {
	case event := <-events:
		if event != "state_changed" {
			t.Fatalf("event = %q, want state_changed", event)
		}
	case <-time.After(time.Second):
		t.Fatal("state_changed was not emitted")
	}
}

func TestEngine_RunStopsOnContextCancel(t *testing.T) {
	engine := New(Deps{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		engine.Run(ctx)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
}

func TestEngine_BrowserPolicyRepublishesAfterFiveSeconds(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, []byte("task_source:\n  type: tcc2_mcp\n  executable: /bin/echo\n  args: [mcp]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	engine := New(Deps{ConfigPath: configPath, BrowserPolicyPath: policyPath})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		engine.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()
	deadline := time.Now().Add(2 * time.Second)
	var first browserpolicy.Policy
	for {
		if payload, err := os.ReadFile(policyPath); err == nil {
			if policy, err := browserpolicy.Decode(payload); err == nil {
				first = policy
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("initial policy was not published")
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(7 * time.Second)
	for {
		current := readBrowserPolicy(t, policyPath)
		if current.Generation > first.Generation && current.UpdatedAt != first.UpdatedAt {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy was not republished: first=%#v current=%#v", first, current)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestEngine_ReloadReturnsErrorForInvalidConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "invalid-config.yml")
	// required field `task_source.executable` is missing.
	if err := os.WriteFile(configPath, []byte(`polling: {interval_seconds: 1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	engine := New(Deps{ConfigPath: configPath})
	result := engine.Reload()
	if result.OK {
		t.Fatalf("Reload() = %#v, want OK false", result)
	}
	if got := len(result.Errors); got == 0 {
		t.Fatal("reload errors should be returned")
	}
}

func TestEngine_PauseAndResumeLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	engine := New(Deps{ConfigPath: configPath})
	if err := engine.Pause(time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Pause() = %v", err)
	}
	if got := engine.Snapshot().State; got != state.StatePaused {
		t.Fatalf("state after pause = %q, want %q", got, state.StatePaused)
	}

	if err := engine.Resume(); err != nil {
		t.Fatalf("Resume() = %v", err)
	}
	if got := engine.Snapshot().State; got != state.StateFetching {
		t.Fatalf("state after resume = %q, want %q", got, state.StateFetching)
	}
}

func TestEngine_PauseReturnsErrorForInvalidTime(t *testing.T) {
	engine := New(Deps{})
	if err := engine.Pause(time.Now().Add(-time.Minute)); err == nil {
		t.Fatal("Pause should reject past times")
	}
}

func TestEngine_Paths(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yml")
	home := t.TempDir()
	t.Setenv("HOME", home)
	engine := New(Deps{ConfigPath: configPath})
	paths := engine.Paths()
	if paths["config"] != configPath {
		t.Fatalf("paths[config] = %q, want %q", paths["config"], configPath)
	}
	expectedPause := filepath.Join(home, constants.StateDirRelPath, constants.PauseFileName)
	if paths["pause"] != expectedPause {
		t.Fatalf("paths[pause] = %q, want %q", paths["pause"], expectedPause)
	}
}

func TestActionResult(t *testing.T) {
	if got := actionResult("a", "accepted", nil); got.Status != "accepted" || got.Code != "" {
		t.Fatalf("actionResult(accepted) = %#v", got)
	}
	if got := actionResult("a", "", errors.New("x")); got.Status != "failed" || got.Code != "action_refused" {
		t.Fatalf("actionResult(err) = %#v", got)
	}
	if got := actionResult("a", "timeout", nil); got.Status != "failed" || got.Code != "timeout" {
		t.Fatalf("actionResult(timeout) = %#v", got)
	}
}

func TestEngine_ResumeReturnsInvalidStateWhenNotPaused(t *testing.T) {
	engine := New(Deps{})
	if err := engine.Resume(); err == nil {
		t.Fatal("Resume should fail when not paused")
	}
}

func TestEngine_ReportActionsCycleMismatchIgnored(t *testing.T) {
	engine := New(Deps{})
	engine.status.CycleID = 5
	accepted, ignored := engine.ReportActions(3, []ActionResult{{ActionID: "a", Status: "accepted"}})
	if accepted != 0 || ignored != 1 {
		t.Fatalf("accepted=%d ignored=%d, want 0 1", accepted, ignored)
	}
}

func TestEngine_ClockRewind_DoesNotMiscalculateElapsed(t *testing.T) {
	machine := state.NewMachine()
	base := time.Now()
	machine.Transition("failure", base, time.Second)
	if transition := machine.Transition("grace_expired", base.Add(-time.Second), time.Second); transition.State != state.StateDegraded {
		t.Fatalf("state=%q want %q", transition.State, state.StateDegraded)
	}
	if transition := machine.Transition("grace_expired", base.Add(2*time.Second), time.Second); transition.State != state.StateReleased {
		t.Fatalf("state=%q want %q", transition.State, state.StateReleased)
	}
}

func TestEngine_Status_AllFieldsPresent_NoEmail(t *testing.T) {
	engine := New(Deps{})
	status := engine.Snapshot()
	if status.State == "" {
		t.Fatal("state is empty")
	}
	if status.CycleID != 0 {
		t.Fatalf("cycle_id=%d, want 0", status.CycleID)
	}
	if status.RunningTasks == nil {
		t.Fatal("running_tasks missing")
	}
	if status.LastError != "" {
		t.Fatalf("last_error=%q, want empty", status.LastError)
	}
}

func TestEngine_ReportActions_Accepted(t *testing.T) {
	engine := New(Deps{})
	engine.status.CycleID = 12
	accepted, ignored := engine.ReportActions(12, []ActionResult{{ActionID: "12-1", Status: "accepted"}})
	if accepted != 1 || ignored != 0 {
		t.Fatalf("accepted=%d ignored=%d, want 1 0", accepted, ignored)
	}
}

func TestEngine_ParseFailure_PreservesRunningTasks(t *testing.T) {
	engine := New(Deps{})
	engine.status.RunningTasks = []TaskView{{Name: "running", TaskID: "task_1234567890abcdef1234567890abcdef", Date: "2026-08-07"}}
	engine.cfg = &config.Config{
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: "/bin/false", Args: nil},
		Polling:    config.Polling{TimeoutSeconds: 1, FailureGraceSeconds: constants.DefaultFailureGraceSeconds, FailurePolicy: "release_controls", IntervalSeconds: constants.MinPollIntervalSeconds},
		Logging:    config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
	}
	if _, err := engine.RunCycleNow(context.Background()); err == nil {
		t.Fatal("RunCycleNow() should fail")
	}
	status := engine.Snapshot()
	if status.ParseOK {
		t.Fatalf("ParseOK = true, want false")
	}
	if got := len(status.RunningTasks); got != 1 {
		t.Fatalf("running_tasks = %d, want 1", got)
	}
}

func TestReportActionsEmitsNotifyForRejectedFrontendAction(t *testing.T) {
	events := make(chan string, 1)
	engine := New(Deps{Emit: func(name string, _ any) { events <- name }})
	engine.status.CycleID = 3
	accepted, ignored := engine.ReportActions(3, []ActionResult{{ActionID: "3-1", Status: "refused", Code: "quit_refused"}})
	if accepted != 1 || ignored != 0 {
		t.Fatalf("accepted=%d ignored=%d", accepted, ignored)
	}
	if name := <-events; name != "notify" {
		t.Fatalf("event = %q, want notify", name)
	}
}

func TestRunCycleNowResumesExpiredPause(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := filepath.Join(home, constants.StateDirRelPath)
	pausePath := filepath.Join(stateDir, constants.PauseFileName)
	if err := state.SavePause(pausePath, state.Pause{Until: time.Now().Add(time.Hour), Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	engine := New(Deps{ConfigPath: filepath.Join(t.TempDir(), "config.yml")})
	if err := state.SavePause(pausePath, state.Pause{Until: time.Now().Add(-time.Second), Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RunCycleNow(context.Background()); err == nil || err.Error() != "config_error" {
		t.Fatalf("error = %v, want config_error", err)
	}
	if got := engine.Snapshot().State; got != state.StateFetching {
		t.Fatalf("state = %q, want %q", got, state.StateFetching)
	}
}

func TestPausePersistFailureDoesNotChangeMemoryState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := filepath.Join(home, constants.StateDirRelPath)
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateDir, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := New(Deps{})
	before := engine.Snapshot().State
	if err := engine.Pause(time.Now().Add(time.Minute)); err == nil {
		t.Fatal("Pause should fail when state directory is invalid")
	}
	if got := engine.Snapshot().State; got != before {
		t.Fatalf("state changed: before=%q after=%q", before, got)
	}
}

func TestDryRunProcessStartDoesNotWriteLedger(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "managed-processes.json")
	value, err := ledger.Load(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	manager := ledger.NewManager(value)
	status, err := manager.Start(context.Background(), ledger.StartSpec{ProcessID: "test-process", Executable: "/not/a/program"}, true)
	if err != nil || status != "skipped" {
		t.Fatalf("Start() = (%q, %v), want (skipped, nil)", status, err)
	}
	if _, ok := value.Get("test-process"); ok {
		t.Fatal("dry run wrote a ledger entry")
	}
}

func TestDryRunCommandRunDoesNotExecute(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Executable: "/not/a/program"}}, true, false)
	if len(results) != 1 || results[0].Status != "skipped" {
		t.Fatalf("results = %#v, want skipped", results)
	}
}

func TestSafetyGateShellDeniedAtRuntime(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Shell: true, Executable: "/bin/echo"}}, false, false)
	if len(results) != 1 || results[0].Status != "failed" || results[0].Code != "permission_denied" {
		t.Fatalf("results = %#v, want permission_denied", results)
	}
}

func TestSafetyGateNonShellActionUnaffected(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Executable: "/usr/bin/true"}}, false, false)
	if len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("results = %#v, want accepted", results)
	}
}

func TestSafetyGateUnmanagedProcessDoesNotSignal(t *testing.T) {
	value, err := ledger.Load(filepath.Join(t.TempDir(), "managed-processes.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := New(Deps{})
	engine.ledger = ledger.NewManager(value)
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{ActionID: "1-1", Kind: "process.stop", ProcessID: "missing"}}, false, false)
	if len(results) != 1 || results[0].Status != "failed" || results[0].Code != "permission_denied" {
		t.Fatalf("results = %#v, want permission_denied", results)
	}
}

func TestSafetyGateForceTerminateNeverInvoked(t *testing.T) {
	value, err := ledger.Load(filepath.Join(t.TempDir(), "managed-processes.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ledger.NewManager(value).Stop(context.Background(), "missing", time.Second, false)
	if err != nil || result.Status != "orphan_dropped" || result.SignalsSent != 0 {
		t.Fatalf("Stop() = (%#v, %v), want no signal", result, err)
	}
}

func TestEngine_ExecuteBackendActions_ProcessStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed-processes.json")
	value, err := ledger.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	engine := New(Deps{})
	engine.ledger = ledger.NewManager(value)
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{ActionID: "1-1", Kind: "process.start", ProcessID: "test-process", Executable: "/bin/sleep", Args: []string{"1000"}}}, false, false)
	if len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("results = %#v, want status accepted", results)
	}
}

func TestEngine_CommandRun_OutputTruncated(t *testing.T) {
	engine := New(Deps{})
	script := createHugeOutputScript(t)
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{
		ActionID:   "1-1",
		Kind:       "command.run",
		Executable: script,
	}}, false, true)
	if len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("results = %#v", results)
	}
	if len(results[0].Detail) != constants.CommandRunMaxOutputBytes {
		t.Fatalf("detail length = %d, want %d", len(results[0].Detail), constants.CommandRunMaxOutputBytes)
	}
}

func TestEngine_CommandRun_Timeout(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Executable: "/bin/sleep", Args: []string{"2"}, TimeoutSeconds: 1}}, false, false)
	if len(results) != 1 || results[0].Status != "failed" || results[0].Code != "timeout" {
		t.Fatalf("results = %#v", results)
	}
}

func TestEngine_CommandRun_NoShellByDefault(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Shell: true, Executable: "/bin/echo", Args: []string{"ok"}}}, false, false)
	if len(results) != 1 || results[0].Status != "failed" || results[0].Code != "permission_denied" {
		t.Fatalf("results = %#v", results)
	}
}

func TestEngine_CommandRun_ShellAllowed(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActions(context.Background(), []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Shell: true, Executable: "/bin/echo", Args: []string{"ok"}}}, false, true)
	if len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("results = %#v", results)
	}
}

func TestEngine_TickTimeout_TreatedAsFailure(t *testing.T) {
	engine := New(Deps{})
	engine.cfg = &config.Config{
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: "/bin/sleep", Args: []string{"1"}},
		Polling:    config.Polling{TimeoutSeconds: 1, FailureGraceSeconds: constants.DefaultFailureGraceSeconds, FailurePolicy: "release_controls", IntervalSeconds: constants.MinPollIntervalSeconds},
		Logging:    config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
		Rules:      []config.Rule{},
	}
	if _, err := engine.RunCycleNow(context.Background()); err == nil {
		t.Fatal("RunCycleNow() should fail")
	}
}

func TestEngine_MCPCrash_TreatedAsFailure(t *testing.T) {
	engine := New(Deps{})
	engine.cfg = &config.Config{
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: "/bin/false", Args: nil},
		Polling:    config.Polling{TimeoutSeconds: 1, FailureGraceSeconds: constants.DefaultFailureGraceSeconds, FailurePolicy: "release_controls", IntervalSeconds: constants.MinPollIntervalSeconds},
		Logging:    config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
		Rules:      []config.Rule{},
	}
	if _, err := engine.RunCycleNow(context.Background()); err == nil {
		t.Fatal("RunCycleNow() should fail")
	}
}

func TestEngine_TCC2NonZeroExit_TreatedAsFailure(t *testing.T) {
	TestEngine_MCPCrash_TreatedAsFailure(t)
}

func TestEngine_TooManyRunningTasks_TreatedAsFailure(t *testing.T) {
	fake := createFakeMCPScriptTooManyTasks(t)
	engine := New(Deps{})
	engine.cfg = &config.Config{
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: fake, Args: []string{"mcp"}},
		Polling:    config.Polling{TimeoutSeconds: 1, FailureGraceSeconds: constants.DefaultFailureGraceSeconds, FailurePolicy: "release_controls", IntervalSeconds: constants.MinPollIntervalSeconds},
		Logging:    config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
		Rules:      []config.Rule{},
	}
	if _, err := engine.RunCycleNow(context.Background()); err == nil {
		t.Fatal("RunCycleNow() should fail")
	}
}

func TestTenThousandCycles(t *testing.T) {
	fake := createFakeMCPScript(t)
	engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
	engine.cfg = &config.Config{
		Version: constants.ConfigSchemaVersion,
		TaskSource: config.TaskSource{
			Type:       "tcc2_mcp",
			Executable: fake,
			Args:       []string{"mcp"},
		},
		Polling: config.Polling{
			IntervalSeconds:     constants.MinPollIntervalSeconds,
			TimeoutSeconds:      1,
			FailureGraceSeconds: constants.DefaultFailureGraceSeconds,
			FailurePolicy:       "release_controls",
		},
		Safety:  config.Safety{},
		Logging: config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
		Rules:   []config.Rule{},
	}
	beforeFD := countOpenFDs(t)
	beforeGoroutine := runtime.NumGoroutine()
	for i := 0; i < 10000; i++ {
		if _, err := engine.RunCycleNow(context.Background()); err != nil {
			t.Fatalf("RunCycleNow(%d) = %v", i, err)
		}
	}
	runtime.GC()
	afterFD := countOpenFDs(t)
	afterGoroutine := runtime.NumGoroutine()
	t.Logf("goroutine_delta == %d fd_delta == %d", afterGoroutine-beforeGoroutine, afterFD-beforeFD)
	if afterGoroutine > beforeGoroutine {
		t.Fatalf("goroutine increase: before=%d after=%d", beforeGoroutine, afterGoroutine)
	}
	if afterFD > beforeFD {
		t.Fatalf("fd increase: before=%d after=%d", beforeFD, afterFD)
	}
}

func createFakeMCPScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-tcc2")
	// Why: This fixture is executed 10,000 times; fixed per-session IDs avoid
	// spawning sed for every request and keep the leak test focused on lifecycle.
	script := "#!/bin/sh\nrequest_id=0\nwhile IFS= read -r line; do\n  case \"$line\" in\n    *'\"method\":\"notifications/initialized\"'*) continue ;;\n  esac\n  request_id=$((request_id + 1))\n  case \"$line\" in\n    *'\"method\":\"initialize\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2025-06-18\",\"serverInfo\":{\"name\":\"fake\"}}}\\n' \"$request_id\" ;;\n    *'\"name\":\"get_user\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"- **Timezone:** UTC\\\\n- **Start of Day:** -05:00:00\"}]}}\\n' \"$request_id\" ;;\n    *'\"name\":\"get_taskchute\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"## 2026-08-07\\\\n- [Done] Finished\"}]}}\\n' \"$request_id\" ;;\n  esac\n done\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func createActiveFakeMCPScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-tcc2-active")
	script := "#!/bin/sh\nrequest_id=0\nwhile IFS= read -r line; do\n  case \"$line\" in\n    *'\"method\":\"notifications/initialized\"'*) continue ;;\n  esac\n  request_id=$((request_id + 1))\n  case \"$line\" in\n    *'\"method\":\"initialize\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2025-06-18\",\"serverInfo\":{\"name\":\"fake\"}}}\\n' \"$request_id\" ;;\n    *'\"name\":\"get_user\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"- **Timezone:** UTC\\\\n- **Start of Day:** -05:00:00\"}]}}\\n' \"$request_id\" ;;\n    *'\"name\":\"get_taskchute\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"## 2026-08-07\\\\n- [In Progress] Active [ID: task_1234567890abcdef1234567890abcdef]\"}]}}\\n' \"$request_id\" ;;\n  esac\n done\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func browserTestConfig(executable string, dryRun bool) *config.Config {
	return &config.Config{
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: executable, Args: []string{"mcp"}},
		Polling:    config.Polling{TimeoutSeconds: constants.DefaultPollTimeoutSeconds, FailureGraceSeconds: 0, FailurePolicy: "release_controls", IntervalSeconds: constants.MinPollIntervalSeconds},
		Safety:     config.Safety{DryRun: dryRun},
		Logging:    config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
		Rules: []config.Rule{{
			ID:    "browser",
			Match: config.Match{TaskNameContains: []string{"Active"}},
			Ensure: []config.Action{{
				Type:    constants.BrowserBlockActionType,
				Domains: []string{"example.com"},
			}},
		}},
	}
}

func readBrowserPolicy(t *testing.T, path string) browserpolicy.Policy {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := browserpolicy.Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func createHugeOutputScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "huge-output.sh")
	body := "#!/bin/sh\nhead -c 70010 /dev/zero | tr '\\0' 'a'\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func createFakeMCPScriptTooManyTasks(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-tcc2-multi")
	var taskLines strings.Builder
	taskLines.WriteString("## 2026-08-07\\n")
	for i := 0; i <= constants.MaxRunningTasks; i++ {
		taskLines.WriteString("- [In Progress] task ")
		taskLines.WriteString(strings.Repeat("x", 16))
		taskLines.WriteString(fmt.Sprintf(" [task_%032x]\\n", i+1))
	}
	taskText := taskLines.String()
	script := "#!/bin/sh\nwhile IFS= read -r line; do\n  case \"$line\" in\n    *'\"method\":\"notifications/initialized\"'*) continue ;;\n    *'\"method\":\"initialize\"'*)\n      id=$(printf '%s\\n' \"$line\" | sed -nE 's/.*\\\"id\\\":([0-9]+).*/\\1/p')\n      printf '{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":%s,\\\"result\\\":{\\\"protocolVersion\\\":\\\"2025-06-18\\\",\\\"serverInfo\\\":{\\\"name\\\":\\\"fake\\\"}}}\\n' \"$id\"\n      ;;\n    *'\"name\":\"get_user\"'*)\n      id=$(printf '%s\\n' \"$line\" | sed -nE 's/.*\\\"id\\\":([0-9]+).*/\\1/p')\n      printf '{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":%s,\\\"result\\\":{\\\"content\\\":[{\\\"type\\\":\\\"text\\\",\\\"text\\\":\\\"- **Timezone:** UTC\\\\n- **Start of Day:** -05:00:00\\\"}]}}\\n' \"$id\"\n      ;;\n    *'\"name\":\"get_taskchute\"'*)\n      id=$(printf '%s\\n' \"$line\" | sed -nE 's/.*\\\"id\\\":([0-9]+).*/\\1/p')\n      printf '{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":%s,\\\"result\\\":{\\\"content\\\":[{\\\"type\\\":\\\"text\\\",\\\"text\\\":%q}]}}\\n' \"$id\" \"" + taskText + "\"\n      ;\n  esac\n done\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := filepath.Glob("/dev/fd/*")
	if err != nil {
		return 0
	}
	return len(entries)
}
