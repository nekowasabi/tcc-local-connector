package engine

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/takets/tcc-local-connector/internal/logging"
	"github.com/takets/tcc-local-connector/internal/rules"
	"github.com/takets/tcc-local-connector/internal/state"
	"github.com/takets/tcc-local-connector/internal/tcc2"
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

func TestNewUsesConfigLoggingLevelAndRetainDays(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, "config.yml")
	body := "version: 2\ntask_source:\n  executable: /bin/echo\nlogging:\n  level: error\n  retain_days: 1\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(home, constants.StateDirRelPath)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := filepath.Join(stateDir, constants.LogFileName+".1")
	keep := filepath.Join(stateDir, constants.LogFileName)
	other := filepath.Join(stateDir, "other.log")
	for _, path := range []string{old, keep, other} {
		if err := os.WriteFile(path, []byte("log"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(old, now.AddDate(0, 0, -2), now.AddDate(0, 0, -2)); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	engine := New(Deps{ConfigPath: configPath, LogWriter: &output})
	engine.logger.Log(logging.Entry{Level: "debug", Event: "debug-dropped"})
	engine.logger.Log(logging.Entry{Level: "info", Event: "info-dropped"})
	engine.logger.Log(logging.Entry{Level: "error", Event: "error-kept"})
	if bytes.Contains(output.Bytes(), []byte("debug-dropped")) || bytes.Contains(output.Bytes(), []byte("info-dropped")) {
		t.Fatalf("below-threshold logs written: %q", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte("error-kept")) {
		t.Fatalf("error log missing: %q", output.String())
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expired log still exists: %v", err)
	}
	for _, path := range []string{keep, other} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
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

func TestRunCycleNowPublishesEnforcedBrowserPolicyWhenTaskIDInvalid(t *testing.T) {
	fake, snapshot := createMutableFakeMCPScript(t)
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	engine := New(Deps{BrowserPolicyPath: policyPath})
	engine.cfg = browserTestConfig(fake, false)

	writeMutableSnapshot(t, snapshot, "## 2026-08-07\\n- [In Progress] Active")
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	missing := readBrowserPolicy(t, policyPath)
	if engine.Snapshot().ParseOK {
		t.Fatal("missing task_id should set parse_ok false")
	}
	if !missing.Enforce || missing.DryRun || !reflect.DeepEqual(missing.Domains, []string{"example.com"}) {
		t.Fatalf("missing task_id policy = %#v", missing)
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
	engine.cfg.TaskSource.Executable = filepath.Join(t.TempDir(), "missing-tcc2")
	if _, err := engine.RunCycleNow(context.Background()); err == nil {
		t.Fatal("RunCycleNow should fail after fetch error")
	}
	// Why: grace_expired uses firstFailure, so a second failed cycle makes now > firstFailure when grace is 0.
	if _, err := engine.RunCycleNow(context.Background()); err == nil {
		t.Fatal("RunCycleNow should fail after fetch error")
	}
	policy := readBrowserPolicy(t, policyPath)
	if policy.Enforce || policy.DryRun || len(policy.Domains) != 0 || len(policy.PlannedDomains) != 0 {
		t.Fatalf("release-controls policy = %#v", policy)
	}
}

func TestReportActionsKeepsBrowserPolicyWhenFrontendActionFails(t *testing.T) {
	fake := createActiveFakeMCPScript(t)
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	var plans []rules.Plan
	engine := New(Deps{BrowserPolicyPath: policyPath, Emit: func(event string, value any) {
		if event == "plan" {
			plans = append(plans, value.(rules.Plan))
		}
	}})
	cfg := browserTestConfig(fake, false)
	cfg.Rules[0].Ensure = append(cfg.Rules[0].Ensure, config.Action{Type: "app.stop", BundleID: "com.example.App"})
	engine.cfg = cfg
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := readBrowserPolicy(t, policyPath)
	if !before.Enforce || !reflect.DeepEqual(before.Domains, []string{"example.com"}) {
		t.Fatalf("setup policy = %#v", before)
	}
	engine.ReportActions(engine.Snapshot().CycleID, []ActionResult{
		{ActionID: "1-1", Status: "refused", Code: "quit_refused"},
	})
	policy := readBrowserPolicy(t, policyPath)
	if !policy.Enforce || policy.DryRun || !reflect.DeepEqual(policy.Domains, []string{"example.com"}) || !reflect.DeepEqual(policy.PlannedDomains, []string{"example.com"}) {
		t.Fatalf("policy after refused app.stop = %#v", policy)
	}
	if policy.Generation != before.Generation {
		t.Fatalf("generation changed from %d to %d", before.Generation, policy.Generation)
	}
	plans = nil
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := readBrowserPolicy(t, policyPath)
	if !after.Enforce || !reflect.DeepEqual(after.Domains, []string{"example.com"}) {
		t.Fatalf("later cycle policy = %#v", after)
	}
	if len(plans) == 0 || !reflect.DeepEqual(plans[len(plans)-1].EnforceStopBundleIDs, []string{"com.example.App"}) {
		t.Fatalf("later cycle stops = %#v", plans)
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

func TestEngine_ReloadAndResumeRestoreEnsureWithoutWaitingInterval(t *testing.T) {
	fake := createActiveFakeMCPScript(t)
	configPath := filepath.Join(t.TempDir(), "config.yml")
	writeEnsureConfig(t, configPath, fake, 3600)
	policyPath := filepath.Join(t.TempDir(), constants.BrowserPolicyFileName)
	var plans []rules.Plan
	engine := New(Deps{ConfigPath: configPath, BrowserPolicyPath: policyPath, Emit: func(event string, value any) {
		if event == "plan" {
			plans = append(plans, value.(rules.Plan))
		}
	}})

	result := engine.Reload()
	if !result.OK {
		t.Fatalf("Reload() = %#v", result)
	}
	afterReload := readBrowserPolicy(t, policyPath)
	if !afterReload.Enforce || !reflect.DeepEqual(afterReload.Domains, []string{"example.com"}) {
		t.Fatalf("reload policy = %#v", afterReload)
	}
	if len(plans) == 0 || !reflect.DeepEqual(plans[len(plans)-1].EnforceStopBundleIDs, []string{"com.example.App"}) {
		t.Fatalf("reload plans = %#v", plans)
	}

	if err := engine.Pause(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	paused := readBrowserPolicy(t, policyPath)
	if paused.Enforce || len(paused.Domains) != 0 {
		t.Fatalf("paused policy = %#v", paused)
	}
	plans = nil
	if err := engine.Resume(); err != nil {
		t.Fatal(err)
	}
	afterResume := readBrowserPolicy(t, policyPath)
	if !afterResume.Enforce || !reflect.DeepEqual(afterResume.Domains, []string{"example.com"}) {
		t.Fatalf("resume policy = %#v", afterResume)
	}
	if len(plans) == 0 || !reflect.DeepEqual(plans[len(plans)-1].EnforceStopBundleIDs, []string{"com.example.App"}) {
		t.Fatalf("resume plans = %#v", plans)
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
	engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{Kind: "command.run", Executable: "/not/a/command"}}, true, false)
	select {
	case name := <-events:
		t.Fatalf("unexpected event %q", name)
	default:
	}
}

func TestExecuteBackendActionsRefusesShellWithoutPermission(t *testing.T) {
	events := make(chan string, 1)
	engine := New(Deps{Emit: func(name string, _ any) { events <- name }})
	engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{Kind: "command.run", Shell: true, Executable: "/not/a/command"}}, false, false)
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
	events := make(chan string, 16)
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

func TestEngine_ReloadInvalidConfigEmitsNotification(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "invalid-config.yml")
	if err := os.WriteFile(configPath, []byte("version: 2\ntask_source:\n  executable: /bin/echo\npolling:\n  interval_seconds: 10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var notifications []map[string]any
	engine := New(Deps{
		ConfigPath:         configPath,
		BrowserPolicyStore: &memoryPolicyStore{},
		Emit: func(event string, value any) {
			if event == "notify" {
				notifications = append(notifications, value.(map[string]any))
			}
		},
	})

	result := engine.Reload()
	if result.OK {
		t.Fatal("Reload() succeeded, want validation error")
	}
	if len(notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notifications))
	}
	if notifications[0]["code"] != "config_error" || notifications[0]["title"] != "設定エラー" {
		t.Fatalf("notification = %#v", notifications[0])
	}
	message, ok := notifications[0]["message"].(string)
	if !ok || !strings.Contains(message, "polling.timeout_seconds") {
		t.Fatalf("notification message = %#v", notifications[0]["message"])
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
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Executable: "/not/a/program"}}, true, false)
	if len(results) != 1 || results[0].Status != "skipped" {
		t.Fatalf("results = %#v, want skipped", results)
	}
}

func TestPlanProjectionOmitsSecrets(t *testing.T) {
	plan := publicPlan(rules.Plan{Actions: []rules.PlannedAction{
		{
			ActionID: "1-1", Phase: "default", Kind: "command.run",
			Executable: "/secret/executable", Args: []string{"secret-arg"},
			WorkingDir: "/secret/workdir", Env: map[string]string{"TOKEN": "secret-env"},
		},
		{
			ActionID: "1-2", Phase: "default", Kind: "notify",
			Title: "Task started", Message: "Public notification",
		},
	}})
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "notify" || plan.Actions[0].Title != "Task started" || plan.Actions[0].Message != "Public notification" {
		t.Fatalf("public plan = %#v", plan)
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-executable", "secret-arg", "secret-workdir", "secret-env"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("public plan leaked %q: %s", secret, payload)
		}
	}
}

func TestBackendActionsReachExecutor(t *testing.T) {
	value, err := ledger.Load(filepath.Join(t.TempDir(), "managed-processes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var logOutput bytes.Buffer
	engine := New(Deps{Logger: logging.New(&logOutput, "info")})
	engine.ledger = ledger.NewManager(value)
	results := engine.executeBackendActionsForCycle(context.Background(), 1, []rules.PlannedAction{
		{ActionID: "1-1", Phase: "default", Kind: "process.start", ProcessID: "worker", Executable: "/bin/sleep", Args: []string{"1000"}},
		{ActionID: "1-2", Phase: "default", Kind: "process.stop", ProcessID: "worker"},
		{ActionID: "1-3", Phase: "rules", Kind: "command.run", Executable: "/usr/bin/true"},
	}, false, false)
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}
	for _, result := range results {
		if result.Status != "accepted" {
			t.Fatalf("results = %#v", results)
		}
	}
	lines := bytes.Split(bytes.TrimSpace(logOutput.Bytes()), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("backend log lines = %d: %s", len(lines), logOutput.String())
	}
	for index, line := range lines {
		var entry logging.Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if entry.CycleID != 1 || entry.ActionID == "" || entry.Phase == "" || entry.ActionType == "" || entry.Status != "accepted" || entry.DurationMS <= 0 {
			t.Fatalf("backend log[%d] = %#v", index, entry)
		}
		if bytes.Contains(line, []byte(`"detail"`)) || bytes.Contains(line, []byte(`"process_id"`)) || bytes.Contains(line, []byte(`"executable"`)) || bytes.Contains(line, []byte(`"args"`)) {
			t.Fatalf("backend log leaked action detail: %s", line)
		}
	}
}

func TestDryRunSkipsSideEffects(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "write-marker.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ran > '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	engine := New(Deps{})
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{
		ActionID: "1-1", Kind: "command.run", Executable: script,
	}}, true, false)
	if len(results) != 1 || results[0].Status != "skipped" {
		t.Fatalf("results = %#v", results)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("dry-run side effect exists: %v", err)
	}
}

func TestTaskSnapshotDeltaInitialAndInvalidPreserve(t *testing.T) {
	first := "task_0123456789abcdef0123456789abcdef"
	current, started, ended, err := observeTaskSnapshot([]tcc2.RunningTask{{TaskID: first}}, nil, false)
	if err != nil || len(current) != 1 || len(started) != 1 || len(ended) != 0 {
		t.Fatalf("initial snapshot = current:%v started:%v ended:%v err:%v", current, started, ended, err)
	}
	if _, _, _, err := observeTaskSnapshot([]tcc2.RunningTask{{TaskID: ""}}, current, true); err == nil {
		t.Fatal("missing task ID was accepted")
	} else if err.reason != "missing" {
		t.Fatalf("missing reason = %q", err.reason)
	}
	if _, _, _, err := observeTaskSnapshot([]tcc2.RunningTask{{TaskID: first}, {TaskID: first}}, current, true); err == nil {
		t.Fatal("duplicate task ID was accepted")
	} else if err.reason != "duplicate" {
		t.Fatalf("duplicate reason = %q", err.reason)
	}
	if len(current) != 1 {
		t.Fatalf("previous snapshot changed: %v", current)
	}
}

func TestTaskSnapshotDeltaEnded(t *testing.T) {
	first := "task_0123456789abcdef0123456789abcdef"
	second := "task_1123456789abcdef0123456789abcdef"
	previous := map[string]struct{}{first: {}}
	current, started, ended, err := observeTaskSnapshot([]tcc2.RunningTask{{TaskID: second}}, previous, true)
	if err != nil || len(current) != 1 || len(started) != 1 || len(ended) != 1 {
		t.Fatalf("replacement snapshot = current:%v started:%v ended:%v err:%v", current, started, ended, err)
	}
	if _, ok := started[second]; !ok {
		t.Fatalf("started=%v, want %s", started, second)
	}
	if _, ok := ended[first]; !ok {
		t.Fatalf("ended=%v, want %s", ended, first)
	}
	current, started, ended, err = observeTaskSnapshot(nil, previous, true)
	if err != nil || len(current) != 0 || len(started) != 0 || len(ended) != 1 {
		t.Fatalf("empty snapshot = current:%v started:%v ended:%v err:%v", current, started, ended, err)
	}
	if _, ok := ended[first]; !ok {
		t.Fatalf("ended=%v, want %s", ended, first)
	}
}

func TestPlanDispatchPreservesOrderAndStopUnion(t *testing.T) {
	fake := createActiveFakeMCPScript(t)
	marker := filepath.Join(t.TempDir(), "default-backend-ran")
	defaultCommand := writeMarkerScript(t, marker)
	var plans []rules.Plan
	engine := New(Deps{
		BrowserPolicyStore: &memoryPolicyStore{},
		Emit: func(event string, value any) {
			if event == "plan" {
				plan := value.(rules.Plan)
				plans = append(plans, plan)
				if len(plan.Actions) != 1 || plan.Actions[0].Kind != "notify" {
					return
				}
				switch plan.Actions[0].Title {
				case "Before backend":
					if _, err := os.Stat(marker); !os.IsNotExist(err) {
						t.Fatalf("frontend action before backend was dispatched late: %v", err)
					}
				case "After backend":
					if _, err := os.Stat(marker); err != nil {
						t.Fatalf("frontend action after backend was dispatched early: %v", err)
					}
				}
			}
		},
	})
	engine.cfg = defaultModeTestConfig(fake, []config.Action{
		{Type: "notify", Title: "Before backend", Message: "Default first"},
		{Type: "command.run", Executable: defaultCommand},
		{Type: "notify", Title: "After backend", Message: "Default second"},
		{Type: "app.stop", BundleID: "com.example.Default"},
	}, []config.Action{
		{Type: "notify", Title: "Rule active", Message: "Rules second"},
		{Type: "app.stop", BundleID: "com.example.Rule"},
	})
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("default backend did not run: %v", err)
	}
	if len(plans) != 5 {
		t.Fatalf("plan events = %d, want 5: %#v", len(plans), plans)
	}
	seenRules := false
	var titles []string
	for _, plan := range plans {
		if !reflect.DeepEqual(plan.EnforceStopBundleIDs, []string{"com.example.Default", "com.example.Rule"}) {
			t.Fatalf("enforce_stop_bundle_ids = %#v", plan.EnforceStopBundleIDs)
		}
		if len(plan.Actions) != 1 {
			t.Fatalf("public actions = %#v", plan.Actions)
		}
		action := plan.Actions[0]
		if action.Phase == "rules" {
			seenRules = true
		} else if seenRules {
			t.Fatalf("default action followed rules action: %#v", plans)
		}
		if action.Kind == "command.run" {
			t.Fatalf("backend action leaked into public plan: %#v", plans)
		}
		titles = append(titles, action.Title)
	}
	if !reflect.DeepEqual(titles[:2], []string{"Before backend", "After backend"}) {
		t.Fatalf("default YAML order changed: %#v", titles)
	}
}

func TestDefaultFailureDoesNotStopRemainingDefaultOrRules(t *testing.T) {
	fake := createActiveFakeMCPScript(t)
	defaultMarker := filepath.Join(t.TempDir(), "default-ran")
	ruleMarker := filepath.Join(t.TempDir(), "rule-ran")
	engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
	engine.cfg = defaultModeTestConfig(fake, []config.Action{
		{Type: "command.run", Executable: "/usr/bin/false"},
		{Type: "command.run", Executable: "/bin/sleep", Args: []string{"2"}, TimeoutSeconds: 1},
		{Type: "command.run", Executable: writeMarkerScript(t, defaultMarker)},
	}, []config.Action{{
		Type: "command.run", Executable: writeMarkerScript(t, ruleMarker),
	}})
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{defaultMarker, ruleMarker} {
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("remaining action did not run (%s): %v", marker, err)
		}
	}
}

func TestDefaultTaskStartAcrossCycles(t *testing.T) {
	fake, snapshotPath := createMutableFakeMCPScript(t)
	first := "task_0123456789abcdef0123456789abcdef"
	second := "task_1123456789abcdef0123456789abcdef"
	var plans []rules.Plan
	engine := New(Deps{
		BrowserPolicyStore: &memoryPolicyStore{},
		Emit: func(event string, value any) {
			if event == "plan" {
				plans = append(plans, value.(rules.Plan))
			}
		},
	})
	engine.cfg = defaultModeTestConfig(fake, []config.Action{{
		Type: "notify", Title: "Task started", Message: "Default",
	}}, nil)

	snapshots := []string{
		runningTaskSnapshot("Active", first),
		runningTaskSnapshot("Active renamed", first),
		runningTaskSnapshot("Active", first) + `\n` + runningTaskLine("Active second", second),
	}
	for _, snapshot := range snapshots {
		writeMutableSnapshot(t, snapshotPath, snapshot)
		if _, err := engine.RunCycleNow(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(plans) != 3 {
		t.Fatalf("plan events = %d, want 3", len(plans))
	}
	defaultCounts := make([]int, len(plans))
	for index, plan := range plans {
		for _, action := range plan.Actions {
			if action.Phase == "default" {
				defaultCounts[index]++
			}
		}
	}
	if !reflect.DeepEqual(defaultCounts, []int{1, 0, 1}) {
		t.Fatalf("default counts = %#v", defaultCounts)
	}
}

func TestInvalidTaskIDObservationKeepsSnapshotAndRunsRules(t *testing.T) {
	cases := []struct {
		name            string
		invalidSnapshot string
		reason          string
	}{
		{name: "missing", invalidSnapshot: "## 2026-08-21\\n- [In Progress] Active", reason: "missing"},
		{name: "duplicate", invalidSnapshot: runningTaskSnapshot("Active", "task_1123456789abcdef0123456789abcdef") + `\n` + runningTaskLine("Active duplicate", "task_1123456789abcdef0123456789abcdef"), reason: "duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, snapshotPath := createMutableFakeMCPScript(t)
			first := "task_0123456789abcdef0123456789abcdef"
			next := "task_2123456789abcdef0123456789abcdef"
			var logOutput bytes.Buffer
			var plans []rules.Plan
			engine := New(Deps{
				BrowserPolicyStore: &memoryPolicyStore{},
				Logger:             logging.New(&logOutput, "info"),
				Emit: func(event string, value any) {
					if event == "plan" {
						plans = append(plans, value.(rules.Plan))
					}
				},
			})
			engine.cfg = defaultModeTestConfig(fake, []config.Action{{
				Type: "notify", Title: "Task started", Message: "Default",
			}}, []config.Action{{
				Type: "notify", Title: "Rule active", Message: "Rules continue",
			}})

			writeMutableSnapshot(t, snapshotPath, runningTaskSnapshot("Active", first))
			if _, err := engine.RunCycleNow(context.Background()); err != nil {
				t.Fatal(err)
			}
			writeMutableSnapshot(t, snapshotPath, tc.invalidSnapshot)
			if _, err := engine.RunCycleNow(context.Background()); err != nil {
				t.Fatalf("invalid observation stopped cycle: %v", err)
			}
			if got := engine.Snapshot().State; got != state.StateActive {
				t.Fatalf("state = %q, want active", got)
			}
			if _, ok := engine.taskIDs[first]; !ok || len(engine.taskIDs) != 1 {
				t.Fatalf("task snapshot changed: %#v", engine.taskIDs)
			}
			invalidPlans := plansForCycle(plans, 2)
			if len(invalidPlans) != 1 || len(invalidPlans[0].Actions) != 1 || invalidPlans[0].Actions[0].Phase != "rules" {
				t.Fatalf("invalid observation plan = %#v", invalidPlans)
			}
			if !strings.Contains(logOutput.String(), `"event":"invalid_task_id_observation"`) || !strings.Contains(logOutput.String(), `"reason":"`+tc.reason+`"`) {
				t.Fatalf("missing invalid observation warning: %s", logOutput.String())
			}
			if strings.Contains(logOutput.String(), first) || strings.Contains(logOutput.String(), "Active") {
				t.Fatalf("invalid observation warning leaked task data: %s", logOutput.String())
			}

			writeMutableSnapshot(t, snapshotPath, runningTaskSnapshot("Active next", next))
			if _, err := engine.RunCycleNow(context.Background()); err != nil {
				t.Fatal(err)
			}
			recoveryPlans := plansForCycle(plans, 3)
			if len(recoveryPlans) != 2 || recoveryPlans[0].Actions[0].Phase != "default" || recoveryPlans[1].Actions[0].Phase != "rules" {
				t.Fatalf("recovery plan = %#v", recoveryPlans)
			}
		})
	}
}

func TestStopTaskEndAcrossCycles(t *testing.T) {
	fake, snapshotPath := createMutableFakeMCPScript(t)
	first := "task_0123456789abcdef0123456789abcdef"
	second := "task_1123456789abcdef0123456789abcdef"
	var plans []rules.Plan
	engine := New(Deps{
		BrowserPolicyStore: &memoryPolicyStore{},
		Emit: func(event string, value any) {
			if event == "plan" {
				plans = append(plans, value.(rules.Plan))
			}
		},
	})
	engine.cfg = lifecycleModeTestConfig(fake, nil, []config.Action{{
		Type: "notify", Title: "Task ended", Message: "Stop",
	}}, nil)

	snapshots := []string{
		runningTaskSnapshot("Active", first),
		runningTaskSnapshot("Active renamed", first),
		runningTaskSnapshot("Active", first) + `\n` + runningTaskLine("Active second", second),
		runningTaskSnapshot("Active second", second),
		emptyTaskSnapshot(),
	}
	for _, snapshot := range snapshots {
		writeMutableSnapshot(t, snapshotPath, snapshot)
		if _, err := engine.RunCycleNow(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(plans) != 5 {
		t.Fatalf("plan events = %d, want 5", len(plans))
	}
	stopCounts := make([]int, len(plans))
	for index, plan := range plans {
		for _, action := range plan.Actions {
			if action.Phase == "stop" {
				stopCounts[index]++
			}
		}
	}
	if !reflect.DeepEqual(stopCounts, []int{0, 0, 0, 1, 1}) {
		t.Fatalf("stop counts = %#v", stopCounts)
	}
}

func TestStopFailureDoesNotStopRemainingStopActions(t *testing.T) {
	fake, snapshotPath := createMutableFakeMCPScript(t)
	first := "task_0123456789abcdef0123456789abcdef"
	stopMarker := filepath.Join(t.TempDir(), "stop-ran")
	ruleMarker := filepath.Join(t.TempDir(), "rule-ran")
	engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
	engine.cfg = lifecycleModeTestConfig(fake, nil, []config.Action{
		{Type: "command.run", Executable: "/usr/bin/false"},
		{Type: "command.run", Executable: writeMarkerScript(t, stopMarker)},
	}, []config.Action{{
		Type: "command.run", Executable: writeMarkerScript(t, ruleMarker),
	}})
	writeMutableSnapshot(t, snapshotPath, runningTaskSnapshot("Active", first))
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeMutableSnapshot(t, snapshotPath, emptyTaskSnapshot())
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{stopMarker, ruleMarker} {
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("remaining action did not run (%s): %v", marker, err)
		}
	}
}

func TestPlanDispatchStopAfterRules(t *testing.T) {
	fake, snapshotPath := createMutableFakeMCPScript(t)
	first := "task_0123456789abcdef0123456789abcdef"
	second := "task_1123456789abcdef0123456789abcdef"
	marker := filepath.Join(t.TempDir(), "stop-backend-ran")
	stopCommand := writeMarkerScript(t, marker)
	var plans []rules.Plan
	engine := New(Deps{
		BrowserPolicyStore: &memoryPolicyStore{},
		Emit: func(event string, value any) {
			if event == "plan" {
				plan := value.(rules.Plan)
				plans = append(plans, plan)
			}
		},
	})
	engine.cfg = lifecycleModeTestConfig(fake, nil, []config.Action{
		{Type: "notify", Title: "Before backend", Message: "Stop first"},
		{Type: "command.run", Executable: stopCommand},
		{Type: "notify", Title: "After backend", Message: "Stop second"},
		{Type: "app.stop", BundleID: "com.example.Stop"},
	}, []config.Action{
		{Type: "notify", Title: "Rule active", Message: "Rules"},
		{Type: "app.stop", BundleID: "com.example.Rule"},
	})
	writeMutableSnapshot(t, snapshotPath, runningTaskSnapshot("Active", first))
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeMutableSnapshot(t, snapshotPath, runningTaskSnapshot("Active", second))
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("stop backend did not run: %v", err)
	}
	endPlans := plansForCycle(plans, 2)
	if len(endPlans) != 5 {
		t.Fatalf("end plan events = %d, want 5: %#v", len(endPlans), endPlans)
	}
	var phases []string
	var titles []string
	seenStop := false
	for _, plan := range endPlans {
		if !reflect.DeepEqual(plan.EnforceStopBundleIDs, []string{"com.example.Rule", "com.example.Stop"}) {
			t.Fatalf("enforce_stop_bundle_ids = %#v", plan.EnforceStopBundleIDs)
		}
		if len(plan.Actions) != 1 {
			t.Fatalf("public actions = %#v", plan.Actions)
		}
		action := plan.Actions[0]
		if action.Phase == "stop" {
			seenStop = true
		} else if seenStop {
			t.Fatalf("rules action followed stop action: %#v", endPlans)
		}
		if action.Kind == "command.run" {
			t.Fatalf("backend action leaked into public plan: %#v", endPlans)
		}
		phases = append(phases, action.Phase)
		titles = append(titles, action.Title)
	}
	if !reflect.DeepEqual(phases, []string{"rules", "rules", "stop", "stop", "stop"}) {
		t.Fatalf("phases = %#v", phases)
	}
	if titles[1] != "Rule active" || titles[2] != "Before backend" || titles[3] != "After backend" {
		t.Fatalf("dispatch order changed: %#v", titles)
	}
}

func TestInvalidTaskIDObservationDoesNotFireStop(t *testing.T) {
	fake, snapshotPath := createMutableFakeMCPScript(t)
	first := "task_0123456789abcdef0123456789abcdef"
	var plans []rules.Plan
	engine := New(Deps{
		BrowserPolicyStore: &memoryPolicyStore{},
		Emit: func(event string, value any) {
			if event == "plan" {
				plans = append(plans, value.(rules.Plan))
			}
		},
	})
	engine.cfg = lifecycleModeTestConfig(fake, nil, []config.Action{{
		Type: "notify", Title: "Task ended", Message: "Stop",
	}}, []config.Action{{
		Type: "notify", Title: "Rule active", Message: "Rules continue",
	}})

	writeMutableSnapshot(t, snapshotPath, runningTaskSnapshot("Active", first))
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeMutableSnapshot(t, snapshotPath, "## 2026-08-21\\n- [In Progress] Active")
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	invalidPlans := plansForCycle(plans, 2)
	if len(invalidPlans) != 1 || len(invalidPlans[0].Actions) != 1 || invalidPlans[0].Actions[0].Phase != "rules" {
		t.Fatalf("invalid observation plan = %#v", invalidPlans)
	}

	writeMutableSnapshot(t, snapshotPath, emptyTaskSnapshot())
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	recoveryPlans := plansForCycle(plans, 3)
	if len(recoveryPlans) != 1 || recoveryPlans[0].Actions[0].Phase != "stop" {
		t.Fatalf("recovery plan = %#v", recoveryPlans)
	}
}

func TestDryRunTaskEndIsNotReplayedWhenExecutionIsEnabled(t *testing.T) {
	fake, snapshotPath := createMutableFakeMCPScript(t)
	taskID := "task_0123456789abcdef0123456789abcdef"
	marker := filepath.Join(t.TempDir(), "stop-ran")
	engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
	engine.cfg = lifecycleModeTestConfig(fake, nil, []config.Action{{
		Type: "command.run", Executable: writeMarkerScript(t, marker),
	}}, nil)
	engine.cfg.Safety.DryRun = true
	writeMutableSnapshot(t, snapshotPath, runningTaskSnapshot("Active", taskID))
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeMutableSnapshot(t, snapshotPath, emptyTaskSnapshot())
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine.cfg.Safety.DryRun = false
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("dry-run stop action was replayed: %v", err)
	}
}

func TestDryRunTaskStartIsNotReplayedWhenExecutionIsEnabled(t *testing.T) {
	fake, snapshotPath := createMutableFakeMCPScript(t)
	taskID := "task_0123456789abcdef0123456789abcdef"
	marker := filepath.Join(t.TempDir(), "default-ran")
	writeMutableSnapshot(t, snapshotPath, runningTaskSnapshot("Active", taskID))
	engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
	engine.cfg = defaultModeTestConfig(fake, []config.Action{{
		Type: "command.run", Executable: writeMarkerScript(t, marker),
	}}, nil)
	engine.cfg.Safety.DryRun = true
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine.cfg.Safety.DryRun = false
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("dry-run action was replayed: %v", err)
	}
}

func TestTaskSnapshotStatePreservedAcrossNonSuccessTransitions(t *testing.T) {
	taskID := "task_0123456789abcdef0123456789abcdef"
	assertPreserved := func(t *testing.T, engine *Engine) {
		t.Helper()
		if !engine.taskIDsSet {
			t.Fatal("task snapshot initialization was reset")
		}
		if _, ok := engine.taskIDs[taskID]; !ok || len(engine.taskIDs) != 1 {
			t.Fatalf("task snapshot changed: %#v", engine.taskIDs)
		}
	}

	t.Run("pause", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
		engine.taskIDs = map[string]struct{}{taskID: {}}
		engine.taskIDsSet = true
		if err := engine.Pause(time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		assertPreserved(t, engine)
	})

	t.Run("fetch failure and release", func(t *testing.T) {
		engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
		engine.taskIDs = map[string]struct{}{taskID: {}}
		engine.taskIDsSet = true
		engine.cfg = defaultModeTestConfig("/path/that/does/not/exist", nil, nil)
		engine.cfg.Polling.FailureGraceSeconds = 0
		if _, err := engine.RunCycleNow(context.Background()); err == nil {
			t.Fatal("fetch failure was accepted")
		}
		assertPreserved(t, engine)
	})

	t.Run("invalid reload", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "invalid.yml")
		if err := os.WriteFile(configPath, []byte("version: ["), 0o600); err != nil {
			t.Fatal(err)
		}
		engine := New(Deps{ConfigPath: configPath, BrowserPolicyStore: &memoryPolicyStore{}})
		engine.taskIDs = map[string]struct{}{taskID: {}}
		engine.taskIDsSet = true
		if result := engine.Reload(); result.OK {
			t.Fatalf("invalid reload = %#v", result)
		}
		assertPreserved(t, engine)
	})
}

func TestSafetyGateShellDeniedAtRuntime(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Shell: true, Executable: "/bin/echo"}}, false, false)
	if len(results) != 1 || results[0].Status != "failed" || results[0].Code != "permission_denied" {
		t.Fatalf("results = %#v, want permission_denied", results)
	}
}

func TestSafetyGateNonShellActionUnaffected(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Executable: "/usr/bin/true"}}, false, false)
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
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{ActionID: "1-1", Kind: "process.stop", ProcessID: "missing"}}, false, false)
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
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{ActionID: "1-1", Kind: "process.start", ProcessID: "test-process", Executable: "/bin/sleep", Args: []string{"1000"}}}, false, false)
	if len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("results = %#v, want status accepted", results)
	}
}

func TestEngine_CommandRun_OutputTruncated(t *testing.T) {
	engine := New(Deps{})
	script := createHugeOutputScript(t)
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{
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
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Executable: "/bin/sleep", Args: []string{"2"}, TimeoutSeconds: 1}}, false, false)
	if len(results) != 1 || results[0].Status != "failed" || results[0].Code != "timeout" {
		t.Fatalf("results = %#v", results)
	}
}

func TestEngine_CommandRun_NoShellByDefault(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Shell: true, Executable: "/bin/echo", Args: []string{"ok"}}}, false, false)
	if len(results) != 1 || results[0].Status != "failed" || results[0].Code != "permission_denied" {
		t.Fatalf("results = %#v", results)
	}
}

func TestEngine_CommandRun_ShellAllowed(t *testing.T) {
	engine := New(Deps{})
	results := engine.executeBackendActionsForCycle(context.Background(), 0, []rules.PlannedAction{{ActionID: "1-1", Kind: "command.run", Shell: true, Executable: "/bin/echo", Args: []string{"ok"}}}, false, true)
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

func createMutableFakeMCPScript(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "snapshot.txt")
	path := filepath.Join(dir, "fake-tcc2-mutable")
	script := fmt.Sprintf(`#!/bin/sh
request_id=0
while IFS= read -r line; do
  case "$line" in
    *'"method":"notifications/initialized"'*) continue ;;
  esac
  request_id=$((request_id + 1))
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake"}}}\n' "$request_id" ;;
    *'"name":"get_user"'*) printf '{"jsonrpc":"2.0","id":%%s,"result":{"content":[{"type":"text","text":"- **Timezone:** UTC\\n- **Start of Day:** -05:00:00"}]}}\n' "$request_id" ;;
    *'"name":"get_taskchute"'*) text=$(cat %q); printf '{"jsonrpc":"2.0","id":%%s,"result":{"content":[{"type":"text","text":"%%s"}]}}\n' "$request_id" "$text" ;;
  esac
done
`, snapshotPath)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path, snapshotPath
}

func runningTaskSnapshot(name, taskID string) string {
	return "## 2026-08-21\\n" + runningTaskLine(name, taskID)
}

func runningTaskLine(name, taskID string) string {
	return "- [In Progress] " + name + " [ID: " + taskID + "]"
}

func writeMutableSnapshot(t *testing.T, path, snapshot string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
}

func plansForCycle(plans []rules.Plan, cycleID int64) []rules.Plan {
	var matches []rules.Plan
	for _, plan := range plans {
		if plan.CycleID == cycleID {
			matches = append(matches, plan)
		}
	}
	return matches
}

func writeMarkerScript(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "write-marker.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf ran > '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func defaultModeTestConfig(executable string, defaultActions, ruleActions []config.Action) *config.Config {
	return lifecycleModeTestConfig(executable, defaultActions, nil, ruleActions)
}

func lifecycleModeTestConfig(executable string, defaultActions, stopActions, ruleActions []config.Action) *config.Config {
	return &config.Config{
		Version: constants.ConfigSchemaVersion,
		TaskSource: config.TaskSource{
			Type:       "tcc2_mcp",
			Executable: executable,
			Args:       []string{"mcp"},
		},
		Polling: config.Polling{
			IntervalSeconds:     constants.MinPollIntervalSeconds,
			TimeoutSeconds:      constants.DefaultPollTimeoutSeconds,
			FailureGraceSeconds: constants.DefaultFailureGraceSeconds,
			FailurePolicy:       "release_controls",
		},
		Safety:  config.Safety{},
		Logging: config.Logging{Level: constants.DefaultLogLevel, RetainDays: constants.DefaultLogRetainDays},
		Default: config.Default{OnTaskStart: defaultActions},
		Stop:    config.Stop{OnTaskEnd: stopActions},
		Rules: []config.Rule{{
			ID:     "active",
			Match:  config.Match{TaskNameContains: []string{"Active"}},
			Ensure: ruleActions,
		}},
	}
}

func emptyTaskSnapshot() string {
	return "## 2026-08-21\\n- [Done] Finished"
}

func writeEnsureConfig(t *testing.T, path, executable string, intervalSeconds int) {
	t.Helper()
	body := fmt.Sprintf("version: 2\ntask_source:\n  type: tcc2_mcp\n  executable: %s\n  args: [mcp]\npolling:\n  interval_seconds: %d\n  timeout_seconds: 20\nsafety:\n  dry_run: false\nrules:\n  - id: browser\n    match:\n      task_name_contains: [Active]\n    ensure:\n      - type: browser.block\n        domains: [example.com]\n      - type: app.stop\n        bundle_id: com.example.App\n", executable, intervalSeconds)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
