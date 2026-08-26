package frontend

import "time"

type State string

const (
	StateStarting             State = "starting"
	StateRunning              State = "running"
	StateBackendDown          State = "backendDown"
	StateBackendDownPermanent State = "backendDownPermanent"
	StateBackendIncompatible  State = "backendIncompatible"
	StateTerminated           State = "terminated"
)

func DescribeState(state State) string {
	switch state {
	case StateStarting:
		return "時計"
	case StateRunning:
		return "チェック"
	case StateBackendDown:
		return "警告"
	case StateBackendDownPermanent:
		return "停止"
	case StateBackendIncompatible:
		return "互換性なし"
	case StateTerminated:
		return "終了"
	default:
		return string(state)
	}
}

type Request struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	Version int            `json:"version"`
	ID      string         `json:"id,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
	Error   *RPCError      `json:"error,omitempty"`
}

type Event struct {
	Version int            `json:"version"`
	Event   string         `json:"event"`
	Data    map[string]any `json:"data,omitempty"`
}

type RunningTask struct {
	Name   string `json:"name"`
	TaskID string `json:"task_id"`
}

type StatusPayload struct {
	State        string        `json:"state"`
	CycleID      int64         `json:"cycle_id"`
	ParseOK      bool          `json:"parse_ok"`
	RunningTasks []RunningTask `json:"running_tasks"`
	LastError    string        `json:"last_error"`
}

type PlanAction struct {
	Kind         string `json:"kind"`
	ActionID     string `json:"action_id"`
	BundleID     string `json:"bundle_id,omitempty"`
	Title        string `json:"title,omitempty"`
	Message      string `json:"message,omitempty"`
	GraceSeconds int    `json:"grace_seconds,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type PlanPayload struct {
	CycleID              int64        `json:"cycle_id"`
	DryRun               bool         `json:"dry_run"`
	Actions              []PlanAction `json:"actions"`
	EnforceStopBundleIDs []string     `json:"enforce_stop_bundle_ids"`
}

type NotifyPayload struct {
	Level   string     `json:"level"`
	Code    string     `json:"code"`
	Title   string     `json:"title"`
	Message string     `json:"message"`
	At      *time.Time `json:"at"`
}

type Snapshot struct {
	State       State
	TaskName    string
	LastUpdated time.Time
	Warning     string
	Paths       map[string]string
}

type Action string

const (
	ActionRefresh             Action = "refresh"
	ActionReloadConfig        Action = "reload_config"
	ActionOpenConfig          Action = "open_config"
	ActionOpenLog             Action = "open_log"
	ActionPauseShort          Action = "pause_short"
	ActionPauseLong           Action = "pause_long"
	ActionPauseUntilNextStart Action = "pause_until_next_start"
	ActionResume              Action = "resume"
	ActionQuit                Action = "quit"
	ActionShowStatus          Action = "show_status"
)
