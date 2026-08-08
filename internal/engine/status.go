package engine

import "github.com/takets/tcc-local-connector/internal/state"

type Status struct {
	State        state.State `json:"state"`
	CycleID      int64       `json:"cycle_id"`
	ParseOK      bool        `json:"parse_ok"`
	RunningTasks []TaskView  `json:"running_tasks"`
	LastError    string      `json:"last_error,omitempty"`
}
type TaskView struct {
	Name   string `json:"name"`
	TaskID string `json:"task_id"`
	Date   string `json:"date"`
}
type TCC2Info struct {
	Version string `json:"version"`
}
type ConfigInfo struct {
	Path string `json:"path"`
}
type UserView struct {
	Timezone string `json:"timezone"`
}
type ManagedProcessView struct {
	ProcessID string `json:"process_id"`
	PID       int    `json:"pid"`
}
