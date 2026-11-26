package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
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

type Deps struct {
	SkipConfigSnapshot bool
	ConfigPath         string
	Emit               func(string, any)
	Logger             *logging.Logger
	LogWriter          io.Writer
	BrowserPolicyPath  string
	BrowserPolicyStore interface {
		Publish(bool, bool, []string, []string) (browserpolicy.Policy, error)
		PublishEmpty() (browserpolicy.Policy, error)
	}
}
type ReloadResult struct {
	OK      bool                     `json:"ok"`
	Applied bool                     `json:"applied"`
	Errors  []config.ValidationError `json:"errors"`
}
type ActionResult struct {
	ActionID string `json:"action_id"`
	Status   string `json:"status"`
	Code     string `json:"code,omitempty"`
	Detail   string `json:"detail,omitempty"`
}
type Engine struct {
	skipConfigSnapshot bool

	mu         sync.Mutex
	configPath string
	cfg        *config.Config
	machine    *state.Machine
	status     Status
	running    bool
	pausePath  string
	ledger     *ledger.Manager
	previous   rules.Evaluation
	taskIDs    map[string]struct{}
	taskIDsSet bool
	emit       func(string, any)
	logger     *logging.Logger
	lastTick   time.Time
	policy     interface {
		Publish(bool, bool, []string, []string) (browserpolicy.Policy, error)
		PublishEmpty() (browserpolicy.Policy, error)
	}
	policyPath string
	policyMode browserPolicyState
	dryRunFP   string
	// Why: raw config.yml bytes last passed to LoadConfig, so the poll loop
	// reloads only on a real edit and an invalid file is not re-reported every tick.
	configSource []byte
}

type browserPolicyState struct {
	enforce bool
	dryRun  bool
	domains []string
	planned []string
}

func New(deps Deps) *Engine {
	path := deps.ConfigPath
	if path == "" {
		path, _ = config.DefaultPath()
	}
	stateDir := defaultStateDir(path)
	level, retainDays := LoggingFromConfig(path)
	logger := deps.Logger
	if logger == nil {
		logger = logging.New(deps.LogWriter, level)
	}
	logger.Rotate(stateDir, retainDays, time.Now())
	failurePath := filepath.Join(stateDir, state.FailureFileName)
	firstFailure, err := state.LoadFailure(failurePath)
	if err != nil {
		logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "failure_state_load_failed", Message: err.Error()})
	}
	engine := &Engine{skipConfigSnapshot: deps.SkipConfigSnapshot, configPath: path, machine: state.NewPersistentMachine(failurePath, firstFailure), status: Status{State: state.StateStarting, RunningTasks: []TaskView{}}, pausePath: filepath.Join(stateDir, constants.PauseFileName), emit: deps.Emit, logger: logger, taskIDs: map[string]struct{}{}}
	engine.policyPath = deps.BrowserPolicyPath
	if engine.policyPath == "" {
		engine.policyPath = defaultBrowserPolicyPath(path)
	}
	engine.policy = deps.BrowserPolicyStore
	if engine.policy == nil {
		engine.policy = browserpolicy.NewStore(engine.policyPath)
	}
	if value, err := ledger.Load(filepath.Join(stateDir, constants.LedgerFileName)); err == nil {
		engine.ledger = ledger.NewManager(value)
	}
	if pause, err := state.LoadPause(engine.pausePath); err == nil && !pause.Until.IsZero() {
		if pause.Expired(time.Now()) {
			_ = state.ClearPause(engine.pausePath)
		} else {
			engine.machine.Transition("pause", time.Now(), 0)
			engine.status.State = engine.machine.Current()
		}
	}
	return engine
}

func LoggingFromConfig(path string) (level string, retainDays int) {
	level = constants.DefaultLogLevel
	retainDays = constants.DefaultLogRetainDays
	cfg, _, err := config.Load(path)
	if err != nil || cfg == nil {
		return
	}
	if cfg.Logging.Level != "" {
		level = cfg.Logging.Level
	}
	if cfg.Logging.RetainDays > 0 {
		retainDays = cfg.Logging.RetainDays
	}
	return
}

func (e *Engine) Logger() *logging.Logger {
	return e.logger
}
func defaultStateDir(configPath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Dir(configPath)
	}
	return filepath.Join(home, constants.StateDirRelPath)
}
func defaultBrowserPolicyPath(configPath string) string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = filepath.Dir(configPath)
	}
	return filepath.Join(configDir, constants.BrowserStateDirRelative, constants.BrowserPolicyFileName)
}
func LogPath(configPath string) string {
	return filepath.Join(defaultStateDir(configPath), constants.LogFileName)
}
func (e *Engine) SetEventSink(emit func(string, any)) { e.mu.Lock(); e.emit = emit; e.mu.Unlock() }
func (e *Engine) emitEvent(name string, data any) {
	e.logger.Log(logging.Entry{Level: "info", Component: "engine", Event: name})
	e.mu.Lock()
	emit := e.emit
	e.mu.Unlock()
	if emit != nil {
		emit(name, data)
	}
}
func (e *Engine) Run(ctx context.Context) {
	_ = e.Reload()
	timer := time.NewTimer(e.pollInterval())
	defer timer.Stop()
	policyTicker := time.NewTicker(constants.BrowserBackendHeartbeatIntervalSeconds * time.Second)
	defer policyTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-policyTicker.C:
			e.republishBrowserPolicy()
		case now := <-timer.C:
			e.reloadIfConfigChanged()
			if e.detectWake(now) {
				tcc2.InvalidateUserCache()
				e.logger.Log(logging.Entry{Level: "info", Component: "engine", Event: "wake_detected"})
			}
			_, _ = e.RunCycleNow(ctx)
			timer.Reset(e.pollInterval())
		}
	}
}

// Why: Reload and resume published empty policy and then waited for the poll timer,
// so matching ensure controls stayed released for a full interval_seconds.
func (e *Engine) cycleNow() {
	_, _ = e.RunCycleNow(context.Background())
}

func (e *Engine) pollInterval() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg == nil || e.cfg.Polling.IntervalSeconds <= 0 {
		return constants.DefaultPollIntervalSeconds * time.Second
	}
	return time.Duration(e.cfg.Polling.IntervalSeconds) * time.Second
}

func (e *Engine) detectWake(now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	interval := constants.DefaultPollIntervalSeconds
	if e.cfg != nil && e.cfg.Polling.IntervalSeconds > 0 {
		interval = e.cfg.Polling.IntervalSeconds
	}
	wasLate := !e.lastTick.IsZero() && now.Sub(e.lastTick) > time.Duration(interval*constants.WakeReevaluateThresholdFactor)*time.Second
	e.lastTick = now
	if wasLate {
		e.machine.Transition("wake", now, 0)
	}
	return wasLate
}
func (e *Engine) Snapshot() Status { e.mu.Lock(); defer e.mu.Unlock(); return e.status }

func (e *Engine) SetDryRun(dryRun bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg == nil {
		return errors.New("config_error")
	}
	if dryRun && !e.cfg.Safety.DryRun && e.status.CycleID > 0 {
		return errors.New("cannot enable dry-run while enforcement is running")
	}
	cfg := *e.cfg
	cfg.Safety.DryRun = dryRun
	e.cfg = &cfg
	return nil
}

func (e *Engine) Reload() ReloadResult {
	result := e.LoadConfig()
	if result.Applied {
		e.cycleNow()
	}
	return result
}

// Why: edits to config.yml must apply without the user pressing reload; the
// caller runs a cycle right after, so this calls LoadConfig rather than Reload.
func (e *Engine) reloadIfConfigChanged() bool {
	data, err := os.ReadFile(e.configPath)
	if err != nil {
		return false
	}
	e.mu.Lock()
	unchanged := bytes.Equal(data, e.configSource)
	if !unchanged {
		e.configSource = data
	}
	e.mu.Unlock()
	if unchanged {
		return false
	}
	e.logger.Log(logging.Entry{Level: "info", Component: "engine", Event: "config_changed"})
	e.LoadConfig()
	return true
}

func (e *Engine) LoadConfig() ReloadResult {
	cfg, source, errs, loadErr := config.LoadWithSource(e.configPath)
	invalid := loadErr != nil || len(errs) > 0
	if loadErr != nil {
		code := "io_error"
		if errors.Is(loadErr, os.ErrNotExist) {
			code = "config_not_found"
		} else if errors.Is(loadErr, config.ErrInsecurePermissions) {
			code = "insecure_permissions"
		}
		errs = []config.ValidationError{{Code: code, Message: loadErr.Error()}}
	}

	e.mu.Lock()
	if source != nil {
		e.configSource = source
	}
	starting := e.cfg == nil
	snapshotPath := filepath.Join(filepath.Dir(e.pausePath), constants.LastGoodConfig)
	if invalid && starting && !e.skipConfigSnapshot {
		cfg, _, _ = config.Load(snapshotPath)
	}
	if cfg == nil {
		if starting {
			e.machine.Transition("config_error", time.Now(), 0)
			e.status.State = e.machine.Current()
		}
		e.mu.Unlock()
		if starting {
			_ = e.publishEmptyBrowserPolicy()
		}
		e.emitEvent("notify", configErrorNotification(errs))
		return ReloadResult{Errors: errs}
	}
	if !invalid && !e.skipConfigSnapshot {
		if err := state.WriteFile(snapshotPath, source); err != nil {
			e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "config_snapshot_failed", Message: err.Error()})
		}
	}
	e.cfg = cfg
	e.machine.Transition("reload_ok", time.Now(), 0)
	e.status.State = e.machine.Current()
	current := e.status
	e.mu.Unlock()

	if starting {
		_ = e.publishEmptyBrowserPolicy()
	}
	if invalid {
		e.emitEvent("notify", configErrorNotification(errs))
	}
	go e.emitEvent("state_changed", current)
	if errs == nil {
		errs = []config.ValidationError{}
	}
	return ReloadResult{OK: !invalid, Applied: true, Errors: errs}
}

func configErrorNotification(validationErrors []config.ValidationError) map[string]any {
	details := make([]string, 0, len(validationErrors))
	for _, validationError := range validationErrors {
		if validationError.Path != "" {
			details = append(details, validationError.Code+": "+validationError.Path)
			continue
		}
		if validationError.Message != "" {
			details = append(details, validationError.Message)
			continue
		}
		details = append(details, validationError.Code)
	}
	return map[string]any{
		"level":   "error",
		"code":    "config_error",
		"title":   "設定エラー",
		"message": "設定エラー: " + strings.Join(details, ", "),
		"at":      time.Now().UTC(),
	}
}
func (e *Engine) RunCycleNow(ctx context.Context) (int64, error) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return 0, errors.New("busy")
	}
	if e.status.State == state.StatePaused {
		pause, err := state.LoadPause(e.pausePath)
		if err != nil || pause.Until.IsZero() || !pause.Expired(time.Now()) {
			e.mu.Unlock()
			_ = e.publishEmptyBrowserPolicy()
			return 0, errors.New("paused")
		}
		if err := state.ClearPause(e.pausePath); err != nil {
			e.mu.Unlock()
			return 0, err
		}
		e.machine.Transition("resume", time.Now(), 0)
		e.status.State = e.machine.Current()
	}
	if e.cfg == nil {
		e.mu.Unlock()
		_ = e.publishEmptyBrowserPolicy()
		return 0, errors.New("config_error")
	}
	e.running = true
	e.status.CycleID++
	id := e.status.CycleID
	e.status.State = state.StateFetching
	cfg := e.cfg
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.running = false; e.mu.Unlock() }()
	fetchCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Polling.TimeoutSeconds)*time.Second)
	defer cancel()
	e.logger.Log(logging.Entry{Level: "info", Component: "engine", Event: "fetch_taskchute"})
	result, err := tcc2.FetchTaskChute(fetchCtx, cfg.TaskSource.Executable, cfg.TaskSource.Args, cfg.TaskSource.ViewID)
	e.mu.Lock()
	if e.status.State == state.StatePaused {
		e.mu.Unlock()
		return id, errors.New("paused")
	}
	if err != nil {
		grace := time.Duration(cfg.Polling.FailureGraceSeconds) * time.Second
		e.recordFetchTransition("failure", grace)
		expiryEvent := "fail_closed"
		if cfg.Polling.FailurePolicy == "release_controls" {
			expiryEvent = "grace_expired"
		}
		transition := e.machine.Transition(expiryEvent, time.Now(), grace)
		e.status.State = e.machine.Current()
		e.status.ParseOK = false
		e.status.LastError = err.Error()
		status := e.status
		e.mu.Unlock()
		switch transition.Code {
		case "grace_expired":
			_ = e.publishEmptyBrowserPolicy()
			if transition.GraceJustExpired {
				e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "controls_released", Reason: "grace_expired"})
			}
			e.emitEvent("state_changed", status)
			e.emitEvent("plan", rules.Plan{CycleID: id, Actions: []rules.PlannedAction{}, EnforceStopBundleIDs: []string{}})
			e.emitEvent("notify", map[string]any{"level": "warn", "code": "grace_expired", "title": "制御を解除しました", "message": "タスク取得の失敗猶予が期限切れになりました", "at": time.Now().UTC()})
		case "fail_closed":
			evaluation := rules.Evaluate(cfg, nil)
			plan, _ := rules.BuildPlan(id, evaluation, evaluation)
			// Why: A failed poll can enforce restrictions without running unrelated ensure or lifecycle actions.
			actions := plan.Actions[:0]
			for _, action := range plan.Actions {
				if action.Kind == "app.stop" {
					actions = append(actions, action)
				}
			}
			plan.Actions = actions
			plan.DryRun = cfg.Safety.DryRun
			if transition.GraceJustExpired {
				e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "fail_closed", Reason: "grace_expired"})
			}
			e.applyBrowserPolicy(plan.DryRun, rules.BuildBrowserPolicy(evaluation))
			e.emitEvent("state_changed", status)
			e.emitEvent("plan", publicPlan(plan))
		default:
			e.emitEvent("state_changed", status)
		}
		return id, err
	}
	taskIDs, started, ended, snapshotErr := observeTaskSnapshot(result.RunningTasks, e.taskIDs, e.taskIDsSet)
	if snapshotErr != nil {
		e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "invalid_task_id_observation", Reason: snapshotErr.reason})
	}
	tasks := make([]TaskView, len(result.RunningTasks))
	for i, task := range result.RunningTasks {
		tasks[i] = TaskView{Name: task.Name, TaskID: task.TaskID, Date: task.Date}
	}
	e.status.RunningTasks = tasks
	e.status.ParseOK = snapshotErr == nil
	if snapshotErr == nil {
		e.status.LastError = ""
	} else {
		e.status.LastError = snapshotErr.Error()
	}
	e.recordFetchTransition("success", 0)
	e.status.State = e.machine.Current()
	previous := e.previous
	current := rules.Evaluate(cfg, result.RunningTasks)
	var defaultActions []config.Action
	if snapshotErr == nil && len(started) > 0 {
		defaultActions = cfg.Default.OnTaskStart
	}
	var stopActions []config.Action
	if snapshotErr == nil && len(ended) > 0 {
		stopActions = cfg.Stop.OnTaskEnd
	}
	plan, _ := rules.BuildPlanWithLifecycle(id, defaultActions, stopActions, previous, current)
	plan.DryRun = cfg.Safety.DryRun
	e.previous = current
	if snapshotErr == nil {
		e.taskIDs = taskIDs
		e.taskIDsSet = true
	}
	plannedDomains := rules.BuildBrowserPolicy(current)
	status := e.status
	e.mu.Unlock()
	e.applyBrowserPolicy(plan.DryRun, plannedDomains)
	e.emitEvent("state_changed", status)
	frontendDispatched := false
	for _, action := range plan.Actions {
		if action.Kind == "process.start" || action.Kind == "process.stop" || action.Kind == "command.run" {
			e.executeBackendActionsForCycle(ctx, id, []rules.PlannedAction{action}, cfg.Safety.DryRun, cfg.Safety.AllowShell)
			continue
		}
		frontendPlan := plan
		frontendPlan.Actions = []rules.PlannedAction{action}
		e.emitEvent("plan", publicPlan(frontendPlan))
		frontendDispatched = true
	}
	if !frontendDispatched {
		emptyPlan := plan
		emptyPlan.Actions = []rules.PlannedAction{}
		e.emitEvent("plan", publicPlan(emptyPlan))
	}
	actionCount := len(plan.Actions)
	if cfg.Safety.DryRun && actionCount > 0 {
		message := fmt.Sprintf("dry_run: %d件のアクションを実行せずスキップしました", actionCount)
		e.logger.Log(logging.Entry{Level: "info", Component: "engine", Event: "dry_run_skipped", Message: message})
		e.emitEvent("notify", map[string]any{
			"level":   "info",
			"code":    "dry_run_skipped",
			"title":   "dry_run のため未実行",
			"message": message,
			"at":      time.Now().UTC(),
		})
	}
	return id, nil
}

func (e *Engine) recordFetchTransition(event string, grace time.Duration) {
	if transition := e.machine.Transition(event, time.Now(), grace); transition.PersistenceError != nil {
		e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "failure_state_save_failed", Message: transition.PersistenceError.Error()})
	}
}

func (e *Engine) applyBrowserPolicy(dryRun bool, plannedDomains []string) {
	if dryRun {
		if err := e.publishBrowserPolicy(false, true, []string{}, plannedDomains); err != nil {
			e.logger.Log(logging.Entry{Level: "error", Component: "engine", Event: "browser_policy_publish_failed", Message: err.Error()})
		}
	} else if len(plannedDomains) == 0 {
		_ = e.publishEmptyBrowserPolicy()
	} else if err := e.publishBrowserPolicy(true, false, plannedDomains, plannedDomains); err != nil {
		e.logger.Log(logging.Entry{Level: "error", Component: "engine", Event: "browser_policy_publish_failed", Message: err.Error()})
	}
}

var taskIDPattern = regexp.MustCompile(constants.TaskIDPattern)

type taskSnapshotError struct {
	reason string
}

func (e *taskSnapshotError) Error() string {
	return "invalid_task_id_observation: " + e.reason
}

func observeTaskSnapshot(tasks []tcc2.RunningTask, previous map[string]struct{}, initialized bool) (map[string]struct{}, map[string]struct{}, map[string]struct{}, *taskSnapshotError) {
	current := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if !taskIDPattern.MatchString(task.TaskID) {
			return nil, nil, nil, &taskSnapshotError{reason: "missing"}
		}
		if _, exists := current[task.TaskID]; exists {
			return nil, nil, nil, &taskSnapshotError{reason: "duplicate"}
		}
		current[task.TaskID] = struct{}{}
	}
	started := map[string]struct{}{}
	ended := map[string]struct{}{}
	if !initialized {
		for taskID := range current {
			started[taskID] = struct{}{}
		}
		return current, started, ended, nil
	}
	for taskID := range current {
		if _, exists := previous[taskID]; !exists {
			started[taskID] = struct{}{}
		}
	}
	for taskID := range previous {
		if _, exists := current[taskID]; !exists {
			ended[taskID] = struct{}{}
		}
	}
	return current, started, ended, nil
}

func publicPlan(plan rules.Plan) rules.Plan {
	actions := make([]rules.PlannedAction, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		if action.Kind != "app.start" && action.Kind != "app.stop" && action.Kind != "notify" {
			continue
		}
		actions = append(actions, rules.PlannedAction{
			ActionID:     action.ActionID,
			Phase:        action.Phase,
			Kind:         action.Kind,
			BundleID:     action.BundleID,
			GraceSeconds: action.GraceSeconds,
			Title:        action.Title,
			Message:      action.Message,
			Reason:       action.Reason,
			Priority:     action.Priority,
		})
	}
	plan.Actions = actions
	return plan
}

func (e *Engine) executeBackendActionsForCycle(ctx context.Context, cycleID int64, actions []rules.PlannedAction, dryRun, allowShell bool) []ActionResult {
	results := make([]ActionResult, 0, len(actions))
	for _, action := range actions {
		startedAt := time.Now()
		result := e.executeBackendAction(ctx, action, dryRun, allowShell)
		results = append(results, result)
		durationMS := time.Since(startedAt).Milliseconds()
		if durationMS == 0 {
			durationMS = 1
		}
		level := "info"
		if result.Status == "failed" {
			level = "warn"
		}
		e.logger.Log(logging.Entry{
			Level:      level,
			Component:  "engine",
			Event:      "backend_action_result",
			CycleID:    cycleID,
			ActionID:   action.ActionID,
			Phase:      action.Phase,
			Status:     result.Status,
			ActionType: action.Kind,
			DurationMS: durationMS,
			ErrorCode:  result.Code,
		})
	}
	return results
}

func (e *Engine) executeBackendAction(ctx context.Context, action rules.PlannedAction, dryRun, allowShell bool) ActionResult {
	// Why: Skip before dispatching instead of relying on each action implementation.
	// A new action kind must inherit dry-run safety by default.
	if dryRun {
		return ActionResult{ActionID: action.ActionID, Status: "skipped"}
	}
	switch action.Kind {
	case "process.start":
		if e.ledger == nil {
			return ActionResult{ActionID: action.ActionID, Status: "failed", Code: "permission_denied"}
		}
		env := make([]string, 0, len(action.Env))
		for key, value := range action.Env {
			env = append(env, key+"="+value)
		}
		sort.Strings(env)
		status, err := e.ledger.Start(ctx, ledger.StartSpec{ProcessID: action.ProcessID, Executable: action.Executable, Args: action.Args, WorkingDir: action.WorkingDir, Env: env}, false)
		return actionResult(action.ActionID, status, err)
	case "process.stop":
		if e.ledger == nil {
			return ActionResult{ActionID: action.ActionID, Status: "failed", Code: "permission_denied"}
		}
		if _, ok := e.ledger.Ledger.Get(action.ProcessID); !ok {
			// Why: Keep the ledger boundary even if a future caller bypasses config validation.
			// Allowing a name-only stop here could target an unrelated process.
			return ActionResult{ActionID: action.ActionID, Status: "failed", Code: "permission_denied"}
		}
		grace := action.GraceSeconds
		if grace == 0 {
			grace = constants.DefaultProcessStopGraceSeconds
		}
		result, err := e.ledger.Stop(ctx, action.ProcessID, time.Duration(grace)*time.Second, false)
		return actionResult(action.ActionID, result.Status, err)
	case "command.run":
		if action.Shell && !allowShell {
			e.emitEvent("notify", map[string]any{"level": "error", "code": "action_refused", "title": "コマンドを拒否しました", "message": "shell 実行は許可されていません", "at": time.Now().UTC()})
			return ActionResult{ActionID: action.ActionID, Status: "failed", Code: "permission_denied"}
		}
		commandCtx := ctx
		var cancel context.CancelFunc
		if action.TimeoutSeconds > 0 {
			commandCtx, cancel = context.WithTimeout(ctx, time.Duration(action.TimeoutSeconds)*time.Second)
			defer cancel()
		}
		command, commandErr := commandRunSpec(commandCtx, action)
		if commandErr != nil {
			return ActionResult{ActionID: action.ActionID, Status: "failed", Code: "action_refused"}
		}
		output, err := runCommand(commandCtx, command)
		if err != nil {
			code := "action_refused"
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
				code = "timeout"
			}
			e.emitEvent("notify", map[string]any{"level": "error", "code": code, "title": "コマンド実行に失敗しました", "message": "設定されたコマンドを実行できませんでした", "at": time.Now().UTC()})
			return ActionResult{ActionID: action.ActionID, Status: "failed", Code: code, Detail: output}
		}
		return ActionResult{ActionID: action.ActionID, Status: "accepted", Detail: output}
	default:
		return ActionResult{ActionID: action.ActionID, Status: "failed", Code: "action_refused"}
	}
}

func actionResult(actionID, status string, err error) ActionResult {
	if err != nil {
		return ActionResult{ActionID: actionID, Status: "failed", Code: "action_refused"}
	}
	if status == "timeout" {
		return ActionResult{ActionID: actionID, Status: "failed", Code: "timeout"}
	}
	return ActionResult{ActionID: actionID, Status: "accepted"}
}

func commandRunSpec(ctx context.Context, action rules.PlannedAction) (*exec.Cmd, error) {
	if action.Shell {
		commandLine := strings.TrimSpace(action.Executable + " " + strings.Join(action.Args, " "))
		return shellCommand(ctx, commandLine), nil
	}
	cmd := exec.CommandContext(ctx, action.Executable, action.Args...)
	cmd.Dir = action.WorkingDir
	return cmd, nil
}

func runCommand(ctx context.Context, command *exec.Cmd) (string, error) {
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	output := truncateOutput(append(stdout.Bytes(), stderr.Bytes()...))
	if err != nil {
		if ctx.Err() != nil {
			return output, ctx.Err()
		}
		return output, err
	}
	return output, nil
}

func truncateOutput(output []byte) string {
	if len(output) <= constants.CommandRunMaxOutputBytes {
		return string(output)
	}
	return string(output[:constants.CommandRunMaxOutputBytes])
}

var (
	ErrPauseDeadline = errors.New("pause deadline must be within 1 hour")
)

func (e *Engine) Pause(until time.Time) error {
	current, err := e.beginPause(until)
	if err != nil {
		e.logger.Log(logging.Entry{Level: "warn", Component: "engine", Event: "paused", Status: "rejected", Message: err.Error()})
		return err
	}
	e.logger.Log(logging.Entry{Level: "info", Component: "engine", Event: "paused", Status: "accepted"})
	policyErr := e.publishEmptyBrowserPolicy()
	go e.emitEvent("state_changed", current)
	go e.emitEvent("plan", rules.Plan{CycleID: current.CycleID, Actions: []rules.PlannedAction{}, EnforceStopBundleIDs: []string{}})
	return policyErr
}

func (e *Engine) beginPause(until time.Time) (Status, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if !until.After(now) || until.After(now.Add(constants.PauseMaxSeconds*time.Second)) {
		return Status{}, ErrPauseDeadline
	}
	// Why: 会議中などに何度でも解除できるよう回数は数えない。長さだけを PauseMaxSeconds で縛る
	if err := state.SavePause(e.pausePath, state.Pause{Until: until, Reason: "user_requested", CreatedAt: now.UTC()}); err != nil {
		return Status{}, err
	}

	e.machine.Transition("pause", now, 0)
	e.status.State = e.machine.Current()
	return e.status, nil
}

func (e *Engine) Resume() error {
	e.mu.Lock()
	if e.status.State != state.StatePaused {
		e.mu.Unlock()
		return errors.New("invalid_state")
	}
	e.mu.Unlock()
	if err := state.ClearPause(e.pausePath); err != nil {
		return err
	}
	e.mu.Lock()
	e.machine.Transition("resume", time.Now(), 0)
	e.status.State = e.machine.Current()
	current := e.status
	e.mu.Unlock()
	policyErr := e.publishEmptyBrowserPolicy()
	go e.emitEvent("state_changed", current)
	e.cycleNow()
	return policyErr
}
func (e *Engine) ReportActions(cycleID int64, results []ActionResult) (int, int) {
	e.mu.Lock()
	if cycleID != e.status.CycleID {
		e.mu.Unlock()
		return 0, len(results)
	}
	e.mu.Unlock()
	for _, result := range results {
		if result.Status == "failed" || result.Status == "refused" || result.Status == "timeout" {
			// Why: Notify the refused action instead of emptying browser policy.
			// app.stop and browser.block are independent ensure actions; Plan.Actions never includes browser.block.
			e.emitEvent("notify", map[string]any{"level": "warn", "code": "action_refused", "title": "アクションを実行できませんでした", "message": "アクション " + result.ActionID + " は " + result.Status + " でした", "at": time.Now().UTC()})
		}
	}
	return len(results), 0
}
func (e *Engine) Paths() map[string]string {
	stateDir := filepath.Dir(e.pausePath)
	return map[string]string{"config": e.configPath, "state_dir": stateDir, "pause": e.pausePath, "ledger": filepath.Join(stateDir, constants.LedgerFileName), "log": LogPath(e.configPath), "browser_policy": e.policyPath}
}

func (e *Engine) publishBrowserPolicy(enforce, dryRun bool, domains, planned []string) error {
	policy, err := e.policy.Publish(enforce, dryRun, domains, planned)
	if err != nil {
		e.mu.Lock()
		e.policyMode = browserPolicyState{domains: []string{}, planned: []string{}}
		e.dryRunFP = ""
		e.mu.Unlock()
		_, _ = e.policy.PublishEmpty()
		return err
	}
	notify := false
	fingerprint := browserpolicy.Fingerprint(policy.PlannedDomains)
	e.mu.Lock()
	e.policyMode = browserPolicyState{enforce: policy.Enforce, dryRun: policy.DryRun, domains: append([]string(nil), policy.Domains...), planned: append([]string(nil), policy.PlannedDomains...)}
	if policy.DryRun && len(policy.PlannedDomains) > 0 {
		notify = fingerprint != e.dryRunFP
		e.dryRunFP = fingerprint
	} else {
		e.dryRunFP = ""
	}
	e.mu.Unlock()
	if notify {
		e.emitEvent("notify", map[string]any{
			"level":   "info",
			"code":    "browser_block_dry_run",
			"title":   "ブラウザ遮断（dry-run）",
			"message": strings.Join(policy.PlannedDomains, ", "),
			"at":      time.Now().UTC(),
		})
	}
	return nil
}

func (e *Engine) publishEmptyBrowserPolicy() error {
	return e.publishBrowserPolicy(false, false, []string{}, []string{})
}

func (e *Engine) republishBrowserPolicy() {
	e.mu.Lock()
	current := e.policyMode
	e.mu.Unlock()
	if err := e.publishBrowserPolicy(current.enforce, current.dryRun, current.domains, current.planned); err != nil {
		e.logger.Log(logging.Entry{Level: "error", Component: "engine", Event: "browser_policy_republish_failed", Message: err.Error()})
	}
}
