package frontend

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Backend interface {
	State() State
	Launch(executable string, args []string) error
	Send(method string, params any) error
	TakeResponses() []Response
	TakePlans() []PlanPayload
	TakeNotifications() []NotifyPayload
	Shutdown()
}

type System interface {
	Open(path string) error
	Terminate()
}

type Controller struct {
	mu       sync.Mutex
	backend  Backend
	system   System
	snapshot Snapshot
	stop     chan struct{}
}

func NewController(backend Backend, system System) *Controller {
	return &Controller{
		backend:  backend,
		system:   system,
		snapshot: Snapshot{State: StateStarting, Paths: map[string]string{}},
		stop:     make(chan struct{}),
	}
}

func (c *Controller) Start(backendPath string, extraArgs []string) error {
	args := append([]string{"serve", "--stdio"}, extraArgs...)
	if err := c.backend.Launch(backendPath, args); err != nil {
		c.setState(StateBackendDown, "バックエンドを起動できません")
		return err
	}
	c.Synchronize()
	go c.loop()
	return nil
}

func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	copySnapshot := c.snapshot
	if c.snapshot.Paths != nil {
		copySnapshot.Paths = map[string]string{}
		for key, value := range c.snapshot.Paths {
			copySnapshot.Paths[key] = value
		}
	}
	return copySnapshot
}

func (c *Controller) Perform(action Action) {
	switch action {
	case ActionRefresh:
		c.send("refresh_now", nil)
	case ActionReloadConfig:
		c.send("reload_config", nil)
	case ActionOpenConfig:
		c.openPath("config")
	case ActionOpenLog:
		c.openPath("log")
	case ActionPauseShort:
		c.send("pause", map[string]any{"duration_seconds": PausePresetShortSeconds})
	case ActionPauseLong:
		c.send("pause", map[string]any{"duration_seconds": PausePresetLongSeconds})
	case ActionPauseUntilNextStart:
		until := nextDayStart(time.Now(), PauseNextDayStartHour)
		c.send("pause", map[string]any{"until": until.Format(time.RFC3339)})
	case ActionResume:
		c.send("resume", nil)
	case ActionQuit:
		c.Quit()
	case ActionShowStatus:
		c.Synchronize()
	}
}

func (c *Controller) Synchronize() {
	_ = c.backend.Send("status", nil)
	_ = c.backend.Send("config_paths", nil)
	c.ConsumePending()
	state := c.backend.State()
	c.mu.Lock()
	if state != "" {
		c.snapshot.State = state
	}
	c.mu.Unlock()
}

func (c *Controller) ConsumePending() {
	for _, notification := range c.backend.TakeNotifications() {
		message := notification.Message
		if message == "" {
			message = notification.Code
		}
		c.mu.Lock()
		c.snapshot.Warning = message
		c.mu.Unlock()
	}
	for _, response := range c.backend.TakeResponses() {
		if response.Error != nil {
			message := response.Error.Message
			if message == "" {
				message = response.Error.Code
			}
			c.mu.Lock()
			c.snapshot.Warning = message
			c.mu.Unlock()
			continue
		}
		if response.Result == nil {
			continue
		}
		if _, ok := response.Result["config"]; ok {
			paths := map[string]string{}
			for key, value := range response.Result {
				if text, ok := value.(string); ok {
					paths[key] = text
				}
			}
			c.mu.Lock()
			c.snapshot.Paths = paths
			c.mu.Unlock()
			continue
		}
		payload, err := json.Marshal(response.Result)
		if err != nil {
			continue
		}
		var status StatusPayload
		if json.Unmarshal(payload, &status) != nil || status.State == "" {
			continue
		}
		taskName := "未取得"
		if len(status.RunningTasks) > 0 && status.RunningTasks[0].Name != "" {
			taskName = status.RunningTasks[0].Name
		}
		c.mu.Lock()
		c.snapshot.TaskName = taskName
		c.snapshot.LastUpdated = time.Now()
		c.mu.Unlock()
	}
	for _, plan := range c.backend.TakePlans() {
		results := make([]map[string]any, 0, len(plan.Actions))
		for _, action := range plan.Actions {
			results = append(results, map[string]any{
				"action_id": action.ActionID,
				"status":    "skipped",
			})
		}
		_ = c.backend.Send("report_actions", map[string]any{
			"cycle_id": plan.CycleID,
			"results":  results,
		})
	}
}

func (c *Controller) Quit() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	c.backend.Shutdown()
	c.system.Terminate()
}

func (c *Controller) loop() {
	ticker := time.NewTicker(time.Duration(MenuRefreshIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.Synchronize()
		}
	}
}

func (c *Controller) send(method string, params any) {
	if err := c.backend.Send(method, params); err != nil {
		c.setState(StateBackendDown, "バックエンドへ要求を送信できません")
	}
}

func (c *Controller) openPath(name string) {
	c.mu.Lock()
	path := c.snapshot.Paths[name]
	c.mu.Unlock()
	if path == "" {
		c.mu.Lock()
		c.snapshot.Warning = "パスを取得中です"
		c.mu.Unlock()
		c.send("config_paths", nil)
		return
	}
	if err := c.system.Open(path); err != nil {
		c.mu.Lock()
		c.snapshot.Warning = fmt.Sprintf("開けません: %s", name)
		c.mu.Unlock()
	}
}

func (c *Controller) setState(state State, warning string) {
	c.mu.Lock()
	c.snapshot.State = state
	c.snapshot.Warning = warning
	c.mu.Unlock()
}

func nextDayStart(now time.Time, hour int) time.Time {
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), hour, 0, 0, 0, now.Location())
}
