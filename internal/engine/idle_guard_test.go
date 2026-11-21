package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/idleguard"
	"github.com/takets/tcc-local-connector/internal/logging"
)

type idleRecorder struct {
	notifies  []map[string]any
	quits     []idleguard.App
	activated int
}

func idleTestEngine(t *testing.T, recorder *idleRecorder, logOutput *strings.Builder, dryRun bool) *Engine {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	engine := New(Deps{
		BrowserPolicyStore: &memoryPolicyStore{},
		Emit: func(name string, data any) {
			if name == "notify" {
				recorder.notifies = append(recorder.notifies, data.(map[string]any))
			}
		},
	})
	if logOutput != nil {
		engine.logger = logging.New(logOutput, "info")
	}
	return engine
}

func idleTestConfig(fake string, graceSeconds int, dryRun bool) *config.Config {
	cfg := defaultModeTestConfig(fake, nil, nil)
	cfg.Safety = config.Safety{DryRun: dryRun}
	cfg.IdleGuard = config.IdleGuard{
		Enabled:      true,
		IdleMinutes:  15,
		GraceSeconds: graceSeconds,
		ExcludeApps:  []string{"Finder"},
	}
	return cfg
}

func idleFakes(recorder *idleRecorder, idle time.Duration) (func() (time.Duration, bool), func() ([]idleguard.App, error), func(idleguard.App) error, func(title, message string) error, func() error) {
	return func() (time.Duration, bool) {
			return idle, true
		}, func() ([]idleguard.App, error) {
			return []idleguard.App{{Name: "Finder"}, {Name: "Safari", BundleID: "com.apple.Safari"}}, nil
		}, func(app idleguard.App) error {
			recorder.quits = append(recorder.quits, app)
			return nil
		}, func(title, message string) error {
			return nil
		}, func() error {
			recorder.activated++
			return nil
		}
}

func notifyCodes(recorder *idleRecorder) []string {
	codes := make([]string, 0, len(recorder.notifies))
	for _, notification := range recorder.notifies {
		code, _ := notification["code"].(string)
		codes = append(codes, code)
	}
	return codes
}

func TestTickIdleGuardWarnThenFireThenCooldown(t *testing.T) {
	fake := createFakeMCPScript(t)
	recorder := &idleRecorder{}
	engine := idleTestEngine(t, recorder, nil, false)
	engine.cfg = idleTestConfig(fake, 2, false)
	engine.idleFor, engine.idleList, engine.idleQuit, engine.idleNotify, engine.idleActivate = idleFakes(recorder, 16*time.Minute)

	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	engine.tickIdleGuard(now)
	if got := notifyCodes(recorder); len(got) != 1 || got[0] != "idle_guard_warn" {
		t.Fatalf("after warn codes = %#v", got)
	}
	if len(recorder.quits) != 0 {
		t.Fatalf("quits before grace = %#v", recorder.quits)
	}
	engine.tickIdleGuard(now.Add(time.Second))
	if len(recorder.quits) != 0 {
		t.Fatalf("early quits = %#v", recorder.quits)
	}
	engine.tickIdleGuard(now.Add(3 * time.Second))
	if len(recorder.quits) != 1 || recorder.quits[0].Name != "Safari" {
		t.Fatalf("quits = %#v", recorder.quits)
	}
	if recorder.activated != 1 {
		t.Fatalf("activated = %d", recorder.activated)
	}
	if got := notifyCodes(recorder); len(got) != 2 || got[1] != "idle_guard_fired" {
		t.Fatalf("after fire codes = %#v", got)
	}
	fired, ok := recorder.notifies[1]["message"].(string)
	if !ok || fired != idleguard.StartMessage {
		t.Fatalf("fire message = %#v", recorder.notifies[1])
	}
	// Cooldown: further idle ticks must not re-fire.
	engine.tickIdleGuard(now.Add(4 * time.Second))
	engine.tickIdleGuard(now.Add(time.Hour))
	if len(recorder.quits) != 1 || recorder.activated != 1 || len(recorder.notifies) != 2 {
		t.Fatalf("cooldown refired: quits=%#v activated=%d notifies=%d", recorder.quits, recorder.activated, len(recorder.notifies))
	}
}

func TestTickIdleGuardCancelsOnTaskStart(t *testing.T) {
	fake, snapshot := createMutableFakeMCPScript(t)
	recorder := &idleRecorder{}
	engine := idleTestEngine(t, recorder, nil, false)
	engine.cfg = idleTestConfig(fake, 60, false)
	engine.idleFor, engine.idleList, engine.idleQuit, engine.idleNotify, engine.idleActivate = idleFakes(recorder, 16*time.Minute)

	writeMutableSnapshot(t, snapshot, emptyTaskSnapshot())
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	engine.tickIdleGuard(now)
	if got := notifyCodes(recorder); len(got) != 1 {
		t.Fatalf("warn codes = %#v", got)
	}
	writeMutableSnapshot(t, snapshot, runningTaskSnapshot("Active", "task_0123456789abcdef0123456789abcdef"))
	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine.tickIdleGuard(now.Add(time.Minute))
	if len(recorder.quits) != 0 || len(recorder.notifies) != 1 {
		t.Fatalf("cancelled cycle fired: quits=%#v notifies=%d", recorder.quits, len(recorder.notifies))
	}
	if engine.guard.PhaseOf() != idleguard.PhaseWatching {
		t.Fatalf("phase = %q", engine.guard.PhaseOf())
	}
}

func TestTickIdleGuardPausedAndUnknownNeverFire(t *testing.T) {
	fake := createFakeMCPScript(t)
	now := time.Now()
	setup := func(t *testing.T) (*Engine, *idleRecorder) {
		recorder := &idleRecorder{}
		engine := idleTestEngine(t, recorder, nil, false)
		engine.cfg = idleTestConfig(fake, 0, false)
		engine.idleFor, engine.idleList, engine.idleQuit, engine.idleNotify, engine.idleActivate = idleFakes(recorder, 16*time.Minute)
		return engine, recorder
	}
	t.Run("paused", func(t *testing.T) {
		engine, recorder := setup(t)
		engine.guardKnown = true
		if err := engine.Pause(now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		engine.tickIdleGuard(now)
		if len(recorder.notifies) != 0 || len(recorder.quits) != 0 {
			t.Fatalf("paused fired: %#v %#v", recorder.notifies, recorder.quits)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		engine, recorder := setup(t)
		engine.guardKnown = false
		engine.tickIdleGuard(now)
		if len(recorder.notifies) != 0 || len(recorder.quits) != 0 {
			t.Fatalf("unknown fired: %#v %#v", recorder.notifies, recorder.quits)
		}
	})
	t.Run("failed fetch marks unknown", func(t *testing.T) {
		engine, recorder := setup(t)
		engine.cfg.TaskSource.Executable = "/nonexistent-tcc2"
		if _, err := engine.RunCycleNow(context.Background()); err == nil {
			t.Fatal("expected fetch failure")
		}
		engine.tickIdleGuard(now)
		if len(recorder.notifies) != 0 || len(recorder.quits) != 0 {
			t.Fatalf("failed fetch fired: %#v %#v", recorder.notifies, recorder.quits)
		}
	})
}

func TestTickIdleGuardDryRunLogsWithoutQuitOrActivate(t *testing.T) {
	fake := createFakeMCPScript(t)
	recorder := &idleRecorder{}
	var logs strings.Builder
	engine := idleTestEngine(t, recorder, &logs, true)
	engine.cfg = idleTestConfig(fake, 0, true)
	engine.idleFor, engine.idleList, engine.idleQuit, engine.idleNotify, engine.idleActivate = idleFakes(recorder, 16*time.Minute)

	if _, err := engine.RunCycleNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine.tickIdleGuard(time.Now())
	if len(recorder.quits) != 0 {
		t.Fatalf("dry-run quits = %#v", recorder.quits)
	}
	if recorder.activated != 0 {
		t.Fatalf("dry-run activated = %d", recorder.activated)
	}
	if got := notifyCodes(recorder); len(got) != 2 || got[0] != "idle_guard_warn" || got[1] != "idle_guard_fired" {
		t.Fatalf("dry-run codes = %#v", got)
	}
	if !strings.Contains(logs.String(), "idle_guard_dry_run") || !strings.Contains(logs.String(), "Safari") {
		t.Fatalf("dry-run log missing quit names: %s", logs.String())
	}
	if strings.Contains(logs.String(), "Finder") {
		t.Fatalf("dry-run log includes excluded app: %s", logs.String())
	}
}

func TestRunCycleNowPushesIdleSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Run("no tasks", func(t *testing.T) {
		engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
		engine.cfg = idleTestConfig(createFakeMCPScript(t), 60, false)
		if _, err := engine.RunCycleNow(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !engine.guardKnown || engine.guardHasTask {
			t.Fatalf("known=%v hasTask=%v", engine.guardKnown, engine.guardHasTask)
		}
	})
	t.Run("running task", func(t *testing.T) {
		engine := New(Deps{BrowserPolicyStore: &memoryPolicyStore{}})
		engine.cfg = idleTestConfig(createActiveFakeMCPScript(t), 60, false)
		if _, err := engine.RunCycleNow(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !engine.guardKnown || !engine.guardHasTask {
			t.Fatalf("known=%v hasTask=%v", engine.guardKnown, engine.guardHasTask)
		}
	})
}
