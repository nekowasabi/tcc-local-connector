package protocol

import "github.com/takets/tcc-local-connector/internal/engine"

// Parameter structures are intentionally kept at the transport boundary.
type statusResult = engine.Status
type reloadConfigResult = engine.ReloadResult
type pauseParams struct {
	DurationSeconds int    `json:"duration_seconds"`
	Until           string `json:"until"`
}
type pauseResult struct {
	Until string `json:"until"`
}
type resumeResult struct {
	Resumed bool `json:"resumed"`
}
type refreshNowResult struct {
	Accepted bool  `json:"accepted"`
	CycleID  int64 `json:"cycle_id"`
}
type configPathsResult = map[string]string
type reportActionsParams struct {
	CycleID int64                 `json:"cycle_id"`
	Results []engine.ActionResult `json:"results"`
}
type reportActionsResult struct {
	Accepted int `json:"accepted"`
	Ignored  int `json:"ignored"`
}
