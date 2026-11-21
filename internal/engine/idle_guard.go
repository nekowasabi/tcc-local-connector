package engine

import (
	"strings"
	"time"

	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/idleguard"
	"github.com/takets/tcc-local-connector/internal/logging"
	"github.com/takets/tcc-local-connector/internal/state"
)

// idleGuardConfig snapshots the idle-guard settings. It reports false when
// the feature is disabled or no configuration is loaded.
func (e *Engine) idleGuardConfig() (idleguard.Config, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg == nil || !e.cfg.IdleGuard.Enabled {
		return idleguard.Config{}, false
	}
	return idleGuardConfigFrom(e.cfg), true
}

func idleGuardConfigFrom(cfg *config.Config) idleguard.Config {
	return idleguard.Config{
		Enabled:      cfg.IdleGuard.Enabled,
		IdleMinutes:  cfg.IdleGuard.IdleMinutes,
		GraceSeconds: cfg.IdleGuard.GraceSeconds,
		ExcludeApps:  append([]string(nil), cfg.IdleGuard.ExcludeApps...),
		DryRun:       cfg.Safety.DryRun,
	}
}

func (e *Engine) systemIdle() (time.Duration, bool) {
	if e.idleFor != nil {
		return e.idleFor()
	}
	return idleguard.SystemIdleDuration()
}

// tickIdleGuard advances the idle auto-quit machine. It runs on the live
// loop ticker only; run-once cycles never tick the guard.
func (e *Engine) tickIdleGuard(now time.Time) {
	cfg, ok := e.idleGuardConfig()
	if !ok {
		if e.guard != nil {
			e.guard.Reset()
		}
		return
	}
	e.mu.Lock()
	paused := e.status.State == state.StatePaused
	known := e.guardKnown
	hasTask := e.guardHasTask
	guard := e.guard
	e.mu.Unlock()
	if guard == nil {
		return
	}
	idle, idleOK := e.systemIdle()
	if !idleOK {
		idle = 0
	}
	for _, effect := range guard.Tick(idleguard.Input{
		Now:            now,
		Idle:           idle,
		HasRunningTask: hasTask,
		TaskKnown:      known,
		Paused:         paused,
	}, cfg) {
		e.applyIdleEffect(effect, cfg)
	}
}

func (e *Engine) applyIdleEffect(effect idleguard.Effect, cfg idleguard.Config) {
	switch effect.Kind {
	case idleguard.EffectWarn:
		e.notifyIdle("idle_guard_warn", effect.Title, effect.Message)
	case idleguard.EffectCancel:
		e.logger.Log(logging.Entry{Level: "info", Component: "engine", Event: "idle_guard_cancelled"})
	case idleguard.EffectFire:
		e.fireIdleGuard(effect, cfg)
	}
}

func (e *Engine) notifyIdle(code, title, message string) {
	if e.idleNotify != nil {
		_ = e.idleNotify(title, message)
	} else {
		// MAC_UNVERIFIED: banner delivery can only be verified on a real Mac.
		_ = idleguard.NotifyUser(title, message)
	}
	// Why: Reuse event.notify so the menu warning text updates like other warnings.
	e.emitEvent("notify", map[string]any{"level": "warn", "code": code, "title": title, "message": message, "at": time.Now().UTC()})
}

func (e *Engine) fireIdleGuard(effect idleguard.Effect, cfg idleguard.Config) {
	list := e.idleList
	if list == nil {
		list = idleguard.ListForegroundApps
	}
	apps, err := list()
	if err != nil {
		e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "idle_guard_list_failed", Message: err.Error()})
	}
	targets := idleguard.FilterApps(apps, cfg.ExcludeApps)
	if effect.DryRun {
		names := make([]string, 0, len(targets))
		for _, app := range targets {
			names = append(names, app.Name)
		}
		e.logger.Log(logging.Entry{Level: "info", Component: "engine", Event: "idle_guard_dry_run", Message: strings.Join(names, ", ")})
	} else {
		quit := e.idleQuit
		if quit == nil {
			quit = idleguard.QuitApp
		}
		for _, app := range targets {
			if err := quit(app); err != nil {
				e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "idle_guard_quit_failed", Message: app.Name})
			}
		}
		activate := e.idleActivate
		if activate == nil {
			activate = idleguard.ActivateTaskChute
		}
		// MAC_UNVERIFIED: TaskChute activation can only be verified on a real Mac.
		if err := activate(); err != nil {
			e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "idle_guard_activate_failed", Message: err.Error()})
		}
	}
	e.notifyIdle("idle_guard_fired", effect.Title, effect.Message)
}
