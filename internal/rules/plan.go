package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/takets/tcc-local-connector/internal/config"
)

type Plan struct {
	CycleID              int64           `json:"cycle_id"`
	DryRun               bool            `json:"dry_run"`
	Actions              []PlannedAction `json:"actions"`
	EnforceStopBundleIDs []string        `json:"enforce_stop_bundle_ids"`
}
type PlannedAction struct {
	ActionID       string            `json:"action_id"`
	Kind           string            `json:"kind"`
	BundleID       string            `json:"bundle_id,omitempty"`
	ProcessID      string            `json:"process_id,omitempty"`
	Executable     string            `json:"executable,omitempty"`
	Args           []string          `json:"args,omitempty"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	GraceSeconds   int               `json:"grace_seconds,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Shell          bool              `json:"shell,omitempty"`
	Title          string            `json:"title,omitempty"`
	Message        string            `json:"message,omitempty"`
	Reason         string            `json:"reason"`
	Priority       int               `json:"priority"`
}
type Conflict struct {
	Target   string `json:"target"`
	Priority int    `json:"priority"`
}

func BuildPlan(cycleID int64, previous, current Evaluation) (Plan, []Conflict) {
	candidates := []PlannedAction{}
	for _, id := range sortedRuleIDs(current.ActiveRuleIDs) {
		rule := current.Rules[id]
		candidates = append(candidates, actions(rule.Ensure, "ensure", rule.Priority)...)
		if !previous.ActiveRuleIDs[id] {
			candidates = append(candidates, actions(rule.OnEnter, "on_enter", rule.Priority)...)
		}
	}
	for _, id := range sortedRuleIDs(previous.ActiveRuleIDs) {
		if !current.ActiveRuleIDs[id] {
			rule := previous.Rules[id]
			candidates = append(candidates, actions(rule.OnExit, "on_exit", rule.Priority)...)
		}
	}
	selected, conflicts := dedupe(candidates)
	for index := range selected {
		selected[index].ActionID = fmt.Sprintf("%d-%d", cycleID, index+1)
	}
	// Why: Union distinct bundle IDs like browser.block domains, instead of
	// treating later app.stop entries as replacements of the first.
	seen := map[string]struct{}{}
	stops := make([]string, 0)
	for _, action := range selected {
		if action.Kind != "app.stop" || action.BundleID == "" {
			continue
		}
		if _, ok := seen[action.BundleID]; ok {
			continue
		}
		seen[action.BundleID] = struct{}{}
		stops = append(stops, action.BundleID)
	}
	sort.Strings(stops)
	return Plan{CycleID: cycleID, Actions: selected, EnforceStopBundleIDs: stops}, conflicts
}
func actions(values []config.Action, reason string, priority int) []PlannedAction {
	output := []PlannedAction{}
	for _, value := range values {
		if value.Type == "app.stop" {
			for _, id := range appStopBundleIDs(value) {
				output = append(output, PlannedAction{Kind: "app.stop", BundleID: id, GraceSeconds: value.GraceSeconds, Reason: reason, Priority: priority})
			}
			continue
		}
		if value.Type != "app.start" && value.Type != "notify" {
			continue
		}
		output = append(output, PlannedAction{Kind: value.Type, BundleID: value.BundleID, ProcessID: value.ProcessID, Executable: value.Executable, Args: value.Args, WorkingDir: value.WorkingDir, Env: value.Env, GraceSeconds: value.GraceSeconds, TimeoutSeconds: value.TimeoutSeconds, Shell: value.Shell, Title: value.Title, Message: value.Message, Reason: reason, Priority: priority})
	}
	return output
}

func appStopBundleIDs(value config.Action) []string {
	if len(value.BundleIDs) > 0 {
		return append([]string(nil), value.BundleIDs...)
	}
	if value.BundleID != "" {
		return []string{value.BundleID}
	}
	return nil
}
func targetKey(action PlannedAction) string {
	if action.Kind == "notify" {
		return action.Kind + ":" + action.Title
	}
	if action.Kind == "command.run" {
		return action.Kind + ":" + action.Executable + "\x00" + strings.Join(action.Args, "\x00") + "\x00" + action.WorkingDir
	}
	if action.ProcessID != "" {
		return "process:" + action.ProcessID
	}
	return "app:" + action.BundleID
}
func dedupe(actions []PlannedAction) ([]PlannedAction, []Conflict) {
	byTarget := map[string]PlannedAction{}
	conflicted := map[string]bool{}
	conflicts := []Conflict{}
	for _, action := range actions {
		key := targetKey(action)
		existing, ok := byTarget[key]
		if !ok || action.Priority > existing.Priority {
			byTarget[key] = action
			continue
		}
		if action.Priority == existing.Priority && action.Kind != existing.Kind {
			if !conflicted[key] {
				conflicts = append(conflicts, Conflict{Target: key, Priority: action.Priority})
				conflicted[key] = true
			}
			delete(byTarget, key)
		}
	}
	output := []PlannedAction{}
	for key, action := range byTarget {
		if !conflicted[key] {
			output = append(output, action)
		}
	}
	sort.SliceStable(output, func(i, j int) bool {
		if output[i].Priority != output[j].Priority {
			return output[i].Priority > output[j].Priority
		}
		return targetKey(output[i]) < targetKey(output[j])
	})
	return output, conflicts
}

func sortedRuleIDs(ids map[string]bool) []string {
	output := make([]string, 0, len(ids))
	for id := range ids {
		output = append(output, id)
	}
	sort.Strings(output)
	return output
}
